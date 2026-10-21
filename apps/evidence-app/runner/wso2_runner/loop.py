"""Main polling loop — runs forever, picks up tasks from cloud, executes them."""

import asyncio
import os
import platform
import time
import traceback
from pathlib import Path

import httpx

from wso2_runner import oauth
from wso2_runner.agent import (
    configure_network_guard,
    execute_task,
    open_login_browser,
    reset_browser,
    update_url_policy,
)
from wso2_runner.azure_credential import ClientAuthenticationError, CredentialUnavailableError
from wso2_runner.client import CloudClient
from wso2_runner.config import RUNNER_VERSION, settings
from wso2_runner.egress_proxy import EgressProxy
from wso2_runner.url_policy import PolicyError, RunnerUrlPolicy


class _PolicyUnavailable(Exception):
    """The URL policy couldn't be loaded. The message says why, in words a
    user can act on."""


async def _load_url_policy(client: CloudClient) -> RunnerUrlPolicy:
    """Fetch and check the URL policy the browser must enforce.

    There is deliberately no fallback: no cached copy and no built-in list.
    A Runner that can't get the current policy runs nothing and asks for a
    retry, rather than enforcing a policy an admin may since have tightened.
    """
    try:
        data = await client.get_url_policy()
    except httpx.HTTPStatusError as exc:
        if exc.response.status_code == 404:
            raise _PolicyUnavailable("the server has no URL policy — it may be older than this Runner") from exc
        raise _PolicyUnavailable(f"the server answered HTTP {exc.response.status_code}") from exc
    except httpx.HTTPError as exc:
        raise _PolicyUnavailable(f"the server couldn't be reached ({type(exc).__name__})") from exc
    except ValueError as exc:
        raise _PolicyUnavailable("the server's answer wasn't a URL policy") from exc
    try:
        return RunnerUrlPolicy.from_json(data)
    except PolicyError as exc:
        raise _PolicyUnavailable(f"the server's URL policy can't be used: {exc}") from exc


_TOO_OLD_FALLBACK = (
    "This Runner is older than the oldest version the server accepts. "
    "Install the latest Runner release, then start it again."
)


def _server_detail(response: httpx.Response) -> str | None:
    try:
        detail = response.json().get("detail")
    except (ValueError, AttributeError):
        return None
    return detail if isinstance(detail, str) and detail else None


def _too_old_message(policy: RunnerUrlPolicy) -> str:
    return (
        f"This Runner is version {RUNNER_VERSION}; the server requires {policy.min_runner_version} or newer. "
        "Install the latest Runner release, then start it again."
    )


async def _fail_task(client: CloudClient, task: dict, message: str) -> None:
    try:
        await client.post_result(task["id"], {"status": "failed", "result": None, "error": message, "screenshots": []})
    except Exception:
        pass


def _runner_id() -> str:
    return f"{platform.node()}-{os.getpid()}"


async def _fail_task_for_azure_auth(task: dict, client: CloudClient, cause: str) -> None:
    """Mark `task` failed with a plain-language message when an Azure Entra
    session dies mid-run, and print a message that's hard to miss in the
    terminal.

    v1 accepted behaviour: the task is failed rather than returned to the
    queue (that needs backend support — out of scope, see #87). The caller
    is responsible for stopping the poll loop afterwards — an expired or
    missing Azure session cannot be fixed by continuing to poll; every
    later task would fail the exact same way.
    """
    message = f"{cause} Run `az login`, then start the Runner again."

    print()
    print("=" * 70)
    print(f"[runner] ✗ AZURE AUTHENTICATION PROBLEM — Task #{task['id']} failed")
    print(f"[runner]   {message}")
    print("[runner]   Stopping — the Runner cannot run further tasks until this is fixed.")
    print("=" * 70)
    print()

    try:
        await client.post_result(task["id"], {
            "status": "failed",
            "result": None,
            "error": message,
            "screenshots": [],
        })
    except Exception:
        pass


async def run_forever(
    cloud_url: str | None = None,
    user_email: str | None = None,
    poll_interval: float | None = None,
) -> None:
    url = cloud_url or settings.CLOUD_URL
    login_hint = user_email or settings.USER_EMAIL
    interval = poll_interval if poll_interval is not None else settings.POLL_INTERVAL
    runner_id = _runner_id()

    if not settings.ASGARDEO_CLIENT_ID:
        print(
            "[runner] ASGARDEO_CLIENT_ID is not set. Register the Runner as a "
            "Native Application in the Asgardeo console and set its client ID "
            "in ~/.wso2-runner/.env — see the setup docs.",
        )
        raise SystemExit(1)

    # Signs in via Asgardeo (browser opens on first run / once the cached
    # session expires) and caches the token for reuse — see wso2_runner/oauth.py.
    oauth.get_access_token(settings.ASGARDEO_ORG, settings.ASGARDEO_CLIENT_ID, login_hint=login_hint)
    email = oauth.current_email() or login_hint or "(unknown — signed in via Asgardeo)"

    client = CloudClient(
        base_url=url,
        asgardeo_org=settings.ASGARDEO_ORG,
        asgardeo_client_id=settings.ASGARDEO_CLIENT_ID,
    )

    print(f"[runner] Server : {url}")
    print(f"[runner] User   : {email}")
    print(f"[runner] ID     : {runner_id}")

    # The URL policy and the egress proxy come first: no browser opens until
    # both are in place (agent.py refuses to create one before then).
    try:
        policy = await _load_url_policy(client)
    except _PolicyUnavailable as exc:
        print(f"[runner] ✗ Couldn't load the URL policy from {url}: {exc}.")
        print("[runner]   Nothing was run. Check your connection, then run `wso2-runner start` again.")
        await client.aclose()
        raise SystemExit(1)
    if policy.requires_newer_than(RUNNER_VERSION):
        print(f"[runner] ✗ {_too_old_message(policy)}")
        await client.aclose()
        raise SystemExit(1)
    proxy = EgressProxy(policy)
    await proxy.start()
    configure_network_guard(policy, proxy)

    print(
        f"[runner] Guard  : {len(policy.allowed_domains)} approved domain(s), "
        f"{len(policy.internal_names) + len(policy.internal_networks)} internal host(s) or range(s)"
    )
    print(f"[runner] Polling every {interval}s — press Ctrl+C to stop")
    print()

    try:
        while True:
            try:
                task = await client.get_next_task(runner_id)

                if task is None:
                    await asyncio.sleep(interval)
                    continue

                print(f"[runner] ── Task #{task['id']} picked up ──────────────────────")
                if task.get("kind") == "login":
                    print(f"[runner] Kind   : login — opening browser at {task['prompt']}")
                elif task.get("kind") == "reset":
                    print("[runner] Kind   : reset — closing browser session")
                else:
                    print(f"[runner] Prompt : {task['prompt'][:100]}{'...' if len(task['prompt']) > 100 else ''}")
                    if task.get("region_hint"):
                        print(f"[runner] Hint   : {task['region_hint']}")
                print()

                # The policy is fetched fresh for every task, so an admin's
                # change applies from the next task on. If it can't be
                # fetched, this task fails with a request to retry it.
                try:
                    policy = await _load_url_policy(client)
                except _PolicyUnavailable as exc:
                    message = (
                        f"The Runner couldn't load the URL policy from the server ({exc}), "
                        "so it didn't run this task. Retry the task."
                    )
                    print(f"[runner] ✗ Task #{task['id']} not run — {message}")
                    print()
                    await _fail_task(client, task, message)
                    continue
                if policy.requires_newer_than(RUNNER_VERSION):
                    message = _too_old_message(policy)
                    print(f"[runner] ✗ Task #{task['id']} not run — {message}")
                    await _fail_task(client, task, message)
                    raise SystemExit(1)
                proxy.set_policy(policy)
                update_url_policy(policy)

                try:
                    outcome = await _run_task_supervised(task, client)
                    if outcome == "cancelled":
                        print(f"[runner] ■ Task #{task['id']} cancelled")
                    else:
                        print(f"[runner] ✓ Task #{task['id']} completed")
                # Azure credential failures are reported by type, not as an
                # ordinary task failure with a raw exception string — and
                # unlike other task failures, the Runner cannot recover by
                # retrying: every subsequent task would fail the same way,
                # so it stops polling and exits instead of continuing.
                # CredentialUnavailableError is a subclass of
                # ClientAuthenticationError — it must be checked first or
                # it's silently swallowed by the wider case.
                except CredentialUnavailableError:
                    await _fail_task_for_azure_auth(
                        task, client,
                        "Azure sign-in isn't available — the Azure CLI isn't installed, "
                        "or nobody is signed in.",
                    )
                    raise SystemExit(1)
                except ClientAuthenticationError:
                    await _fail_task_for_azure_auth(
                        task, client,
                        "Your Azure session has expired or was rejected — you may be "
                        "signed in to the wrong tenant, or lack access to Azure OpenAI.",
                    )
                    raise SystemExit(1)
                except Exception as exc:
                    print(f"[runner] ✗ Task #{task['id']} failed: {exc}")
                    traceback.print_exc()
                    try:
                        await client.post_result(task["id"], {
                            "status": "failed",
                            "result": None,
                            "error": str(exc)[:1000],
                            "screenshots": [],
                        })
                    except Exception:
                        pass

                print()

            except httpx.ConnectError:
                print(f"[runner] Cannot reach {url} — retrying in 10s...")
                await asyncio.sleep(10)
            except httpx.HTTPStatusError as exc:
                # The server refuses tasks to a Runner older than its minimum
                # version (426). Retrying can't fix that, so say why and stop.
                if exc.response.status_code == 426:
                    print(f"[runner] ✗ {_server_detail(exc.response) or _TOO_OLD_FALLBACK}")
                    raise SystemExit(1)
                print(f"[runner] HTTP {exc.response.status_code} — retrying in 5s...")
                await asyncio.sleep(5)
            except Exception as exc:
                print(f"[runner] Unexpected error: {exc} — retrying in 5s...")
                await asyncio.sleep(5)

    finally:
        await proxy.close()
        await client.aclose()


async def _run_task_supervised(task: dict, client: CloudClient) -> str:
    """Run one task while keeping the backend/UI aware that we're alive.

    Returns "cancelled" if the task was stopped from the UI, else "completed".

    The poll loop is paused during execution, so we send a periodic heartbeat
    (otherwise the UI shows the runner as offline). The same heartbeat tells us
    if the task was cancelled from the UI — if so, we abort it promptly instead
    of running to the end. Only the transport layer is touched here; the browser
    automation in agent.py is unchanged, just cancelled from the outside.
    """
    task_id = task["id"]

    # Immediate heartbeat so the UI flips to "online" the instant work starts.
    try:
        await client.heartbeat(task_id)
    except Exception:
        pass

    run = asyncio.ensure_future(_run_task(task, client))

    async def _watch() -> None:
        while not run.done():
            await asyncio.sleep(8)
            if run.done():
                return
            try:
                if await client.heartbeat(task_id):
                    run.cancel()  # cancelled from the UI — stop the task now
                    return
            except Exception:
                pass  # transient network blip — keep the task running

    watch = asyncio.ensure_future(_watch())
    try:
        await run
        return "completed"
    except asyncio.CancelledError:
        print(f"[runner] ■ Task #{task_id} cancelled from the UI")
        try:
            await client.post_result(task_id, {
                "status": "cancelled",
                "result": None,
                "error": None,
                "screenshots": [],
            })
        except Exception:
            pass
        return "cancelled"
    finally:
        watch.cancel()
        try:
            await watch
        except asyncio.CancelledError:
            pass


async def _run_task(task: dict, client: CloudClient) -> None:
    """Execute one task end-to-end: run browser, post progress, upload screenshots, post result."""

    if task.get("kind") == "login":
        result = await open_login_browser(task["prompt"])
        await client.post_result(task["id"], result)
        return

    # No merged backend route emits kind == "reset" yet — it's produced by the
    # web app's reset-session control, which lands in a later PR. The handler
    # ships ahead of its emitter so the Runner is ready when that PR merges.
    if task.get("kind") == "reset":
        result = await reset_browser()
        await client.post_result(task["id"], result)
        return

    # Mutable list shared between execute_task and the callback
    subtask_states: list[dict] = []
    total_usage: dict = {}

    async def on_subtask_done(
        states: list,
        idx: int,
        local_paths: list[Path],
        usage: dict,
    ) -> None:
        # Upload each screenshot and post progress immediately so UI updates one by one
        uploaded_any = False
        for i, local_path in enumerate(local_paths or []):
            if not local_path or not local_path.exists():
                continue
            try:
                file_url, file_name = await client.upload_screenshot(local_path)
                states[idx]["screenshots"].append({
                    "file_name": file_name,
                    "file_url": file_url,
                    "subtask": states[idx]["text"][:120],
                    "subtask_index": idx + 1,
                    "scroll_index": i + 1,
                })
                print(f"[runner]   subtask {idx + 1}: screenshot {i + 1} uploaded → {file_url}")
            except Exception as exc:
                print(f"[runner]   subtask {idx + 1}: screenshot {i + 1} upload failed: {exc}")
                continue

            local_path.unlink(missing_ok=True)
            uploaded_any = True
            # Post progress after every screenshot so the UI shows them one by one
            try:
                await client.post_progress(task["id"], {
                    "subtasks": states,
                    "current_index": idx,
                    "total_usage": usage,
                })
            except Exception as exc:
                print(f"[runner]   progress post failed: {exc}")

        # No screenshots this call (status flip to "running", discovery start,
        # or template-subtask expansion) — still post once so the UI reflects
        # it instead of waiting for the next screenshot upload.
        if not uploaded_any:
            try:
                await client.post_progress(task["id"], {
                    "subtasks": states,
                    "current_index": idx,
                    "total_usage": usage,
                })
            except Exception as exc:
                print(f"[runner]   progress post failed: {exc}")

    async def on_pause(states: list, idx: int, usage: dict) -> None:
        """Called by execute_task when a {PAUSE} step completes. Tells the UI the
        task is paused (so it shows the Resume banner), then waits — polling the
        backend — until the user clicks Resume, the task is cancelled, or a 15 min
        safety cap elapses. The browser sits idle meanwhile, so the user can set
        up filters manually in the (headful) runner window."""
        try:
            await client.post_progress(task["id"], {
                "subtasks": states, "current_index": idx, "total_usage": usage,
                "paused": True,
                "pause_message": "Paused — set up your filters in the browser, then click Resume.",
            })
        except Exception as exc:
            print(f"[runner]   pause progress post failed: {exc}")

        print(f"[runner] ⏸ Task #{task['id']} paused at subtask {idx + 1} — waiting for Resume (up to 15 min)")
        start = time.monotonic()
        while time.monotonic() - start < 900:  # 15-minute safety cap
            await asyncio.sleep(2)
            try:
                state = await client.pause_poll(task["id"])
            except Exception:
                continue  # transient network blip — keep waiting
            if state.get("cancelled"):
                raise asyncio.CancelledError()  # let the supervisor finalize as cancelled
            if state.get("resume_requested"):
                print(f"[runner] ▶️ Task #{task['id']} resumed")
                break
        else:
            print(f"[runner] ⏱ Task #{task['id']} pause timed out after 15 min — continuing")

        try:
            await client.post_progress(task["id"], {
                "subtasks": states, "current_index": idx, "total_usage": usage,
                "paused": False,
            })
        except Exception:
            pass

    result = await execute_task(task, on_subtask_done, on_pause)
    await client.post_result(task["id"], result)
