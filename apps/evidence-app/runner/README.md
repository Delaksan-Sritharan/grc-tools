# Compliance Evidence Portal — Runner

`wso2-runner` is the local browser-automation agent. It runs on the engineer's
own machine (not in Docker or Choreo) because it needs a headful Chromium for
SSO/MFA login and OS-level screen capture. It polls the backend for tasks,
drives cloud consoles, captures evidence screenshots, and posts results back.

## Why it runs locally

- **Headful Chromium** with a persistent profile (`~/.wso2-runner/browser_profile/`)
  keeps you logged in across tasks after a single manual MFA login.
- **OS screen capture** (`mss`) needs a real display, so this cannot run in a
  container.

## Prerequisites

- **Google Chrome.** By default the Runner drives the machine's own Google
  Chrome (`BROWSER_CHANNEL=chrome`), not a bundled browser, so Chrome needs to
  already be installed. If you can't install Chrome on this machine, set
  `BROWSER_CHANNEL=chromium` instead — that makes the Runner download and use
  its own Playwright-managed browser, no Chrome required. See
  [`.env.example`](.env.example).
- **[uv](https://docs.astral.sh/uv/)**, to install and run the Runner. See below.

## Install

Install with [`uv`](https://docs.astral.sh/uv/), which supplies its own
Python, so you don't need Python 3.11 on the machine yourself, and no clone of
this repository is needed either.

```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
uv tool install <link to the latest runner wheel>
wso2-runner configure --server https://<portal address>
az login
wso2-runner doctor
wso2-runner start
```

`--server` fetches the Asgardeo org and client ID for that backend and saves
them, along with `CLOUD_URL`, to `~/.wso2-runner/.env` for you — nothing to
hand edit. `configure` also asks for your WSO2 email and saves it as
`USER_EMAIL`, so `wso2-runner start` needs no email argument.

Instead of the installer script, `uv` can also be installed with a package
manager:

- macOS: `brew install uv`
- Windows: `winget install astral-sh.uv`
- Linux: your distribution's package, where one exists (e.g. `pacman -S uv`
  on Arch), otherwise the installer script above

Offering both matters here: a security or compliance team is entitled to
push back on piping a script into a shell, and it would be inconsistent to
ask that question of our own tooling while handing users only that same
pattern for `uv` with no alternative.

`<link to the latest runner wheel>` and `<portal address>` above are
placeholders. Neither is published yet — this section will be updated with
real values once a release exists.

### If you installed with the old `install.sh`

`install.sh` has been removed. If you ran it before, you have a launcher at
`~/.local/bin/wso2-runner` that points at `~/.wso2-runner/venv`, and it will
shadow the `uv`-installed one — commands will keep running the old code.
Remove both:

```bash
rm ~/.local/bin/wso2-runner
rm -rf ~/.wso2-runner/venv
```

Your `~/.wso2-runner/.env` is unaffected — it isn't touched by this cleanup.
Once the old launcher and venv are gone, the cloned repo folder can be
deleted too; it's no longer needed to run the Runner.

## Developer install

For local development against a source checkout, the editable install still
works, and this is what a contributor should use instead of `install.sh`:

```bash
python3.11 -m venv venv && source venv/bin/activate
pip install -e .
python -m playwright install chromium
```

## Configuration

Settings load from `runner/.env` then `~/.wso2-runner/.env` (the latter
wins). Under a wheel install there is no `runner/.env` to find — the wheel
carries no such file inside site-packages — so only `~/.wso2-runner/.env` is
read in practice; that's the intended behaviour, not a gap. See
[`.env.example`](.env.example). The runner authenticates to the backend via
Asgardeo (PKCE) using its own native-app client ID. `wso2-runner configure
--server <url>` fills in `CLOUD_URL`, `ASGARDEO_ORG` and `ASGARDEO_CLIENT_ID`
for you; re-running `configure` updates the existing file in place rather
than replacing it, so it never erases settings the wizard doesn't ask about.

## Layout

```
wso2_runner/
  cli.py             Typer CLI: start / configure / doctor
  loop.py            Polling loop — claims and runs one task at a time
  agent.py           Chromium session, LLM factory, screenshot capture, Azure helpers
  client.py          httpx wrapper for backend REST calls
  oauth.py           Asgardeo PKCE login
  config.py          Settings loaded from environment
  env_file.py        Reads and updates ~/.wso2-runner/.env in place
  browser_install.py Installs Playwright's bundled Chromium on the "chromium" channel
  capture_check.py   `doctor`'s screen-capture sanity check
  url_policy.py      The URL policy from the backend, and the fixed address floor
  egress_proxy.py    Local proxy every browser connection goes through
```

## Where the browser may go

The Runner's browser is limited in two ways, both enforced on your machine:

- **Approved portals.** The browser only opens pages on the domains the
  backend's URL policy approves (`GET /api/agent/policy`). Admins manage that
  list in the backend's settings; it is fetched when the Runner starts and
  again before every task. If it can't be fetched, nothing runs: at startup
  the Runner exits and asks you to start it again, and for a task, the task
  fails and asks you to retry it. There is no cached or built-in copy.
- **No local or private addresses.** Every connection the browser makes —
  pages, redirects, scripts, images — goes through a small proxy the Runner
  starts on `127.0.0.1`. The proxy resolves each name itself and refuses the
  connection if any address is loopback, link-local (including cloud
  metadata), multicast or reserved — always, whatever the policy says — or a
  private address the policy doesn't allow. A private address is allowed
  through one of the policy's internal host names, or when it lies inside one
  of its internal ranges. The proxy connects only to an address it checked,
  so a name that changes its answer can't slip through. Plain-http requests
  are refused.

Before its first task, the browser fetches a test address only the proxy
can see. If that request doesn't arrive — for example because a policy on
the machine overrides the browser's proxy settings — the Runner refuses to
run anything. Blocked connections are printed in the terminal as
`[guard] Blocked …`.

Internal portals are added by an admin to the backend's
`AGENT_INTERNAL_HOSTS` (host names, addresses or ranges), not configured on
your machine. A range lets connections reach the addresses inside it, but a
page opened by a raw address — `https://10.20.5.7/` — needs that address
listed on its own, because the browser can't match pages against a range.

This needs a backend that serves `GET /api/agent/policy`; against an older
backend the Runner exits at startup and says so.

## LLM providers

`AGENT_PROVIDER` selects the model backend: `azure`, `anthropic`, `gemini`, or
`ollama`.

**Azure (the normal path) needs no key.** Run `az login` once, and set
`AZURE_OPENAI_ENDPOINT`, `AZURE_OPENAI_DEPLOYMENT` and `AZURE_TENANT_ID` in
your `.env` (see `.env.example`). The Runner then authorises every Azure
OpenAI call with your own signed-in Azure identity — no credential is ever
written to disk. `wso2-runner configure` asks for these and reminds you to
run `az login`; it never asks for a key.

Access comes from the **Cognitive Services OpenAI User** role on the Azure
OpenAI resource. Today that role is granted **per person**, so a new joiner
has to be granted it individually before their first run, and it has to be
removed individually when they leave. Ask whoever owns the Azure OpenAI
resource to grant it to you.

Moving this to a single Entra group, so joining the group is all that is
needed, is planned but not done. Until then, `wso2-runner doctor` and
`wso2-runner start` both check that you can actually call the endpoint, so
a missing role shows up before a run starts rather than part-way through
one.

Anthropic and Gemini still use a plain API key — set the matching key in
your `.env`. Ollama needs no key at all.

### When your Azure sign-in expires

Azure sign-ins expire periodically (governed by WSO2 IT's Conditional
Access policy — there's no fixed number to plan around). You don't need to
watch for this yourself: `wso2-runner start` checks your Azure sign-in
*before* it starts polling for tasks, and if a task is already running when
your session expires mid-run, the Runner fails that task with a plain
message and stops rather than continuing to poll uselessly. Either way, the
fix is the same:

```bash
az login
```

Then start the Runner again.

### `wso2-runner doctor` — Azure access states

In `entra` mode (the default), `doctor` signs in to Azure and then makes one
real, free call to your Azure OpenAI endpoint. Both halves matter: signing in
proves who you are, and only the call proves you are allowed to use it. Azure
hands a token to anyone in the tenant, and the role assignment is checked by
the endpoint when the token is presented — so a sign-in that succeeds is not
on its own proof that anything will work.

| State | Meaning | Fix |
| --- | --- | --- |
| Azure CLI is not installed | `az` isn't on your machine | Install the Azure CLI, then run `az login` |
| Azure CLI is installed but nobody is signed in | No cached session | Run `az login` |
| Signed in to the wrong tenant | Rejected at sign-in. Your CLI holds a session, but not for the tenant in your config | Run `az account show`, compare its `tenantId` to `AZURE_TENANT_ID`, then `az login --tenant <AZURE_TENANT_ID>` |
| Signed in, but not allowed to call Azure OpenAI | You are authenticated and the endpoint refused the call. You are missing the Azure OpenAI role | Ask an administrator to grant you the role. `az login` will not fix this |
| Access could not be confirmed | `AZURE_OPENAI_ENDPOINT` isn't set, or it couldn't be reached | Set the endpoint, or check your network. The Runner still starts |
| Azure OpenAI access works | Everything is working | Nothing to do |

`wso2-runner start` runs the same check before it takes any work, so a missing
role stops you at the terminal rather than halfway through a task. The one
exception is "access could not be confirmed": that only warns, and the Runner
starts anyway — a check that can't reach a verdict must not stop a Runner that
would have worked.

In `api_key` mode (rollout-period only), `doctor` just checks that
`AZURE_OPENAI_API_KEY` is set, same as before.
