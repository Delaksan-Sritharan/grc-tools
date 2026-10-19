"""
`POST /agent/tasks` refuses a portal URL a Runner should never open.

A `login` task's `prompt` is opened as-is by the Runner in a real browser, and
a `run` task's `portal_url` names the portal the agent is told it is on. Until
now any URL was stored, so a direct API call could queue one for the Runner to
open. `app/portal_url.py` now applies the rules when the task is created: a
public https page, no credentials, and no local, private, link-local or cloud
metadata host.

The host has to be judged the way the browser that opens it will read it, so
the numeric shorthand forms Chrome accepts (`127.1`, `0x7f.1`, `2130706433`,
IPv6 forms carrying an IPv4 address) are covered below alongside the plain
ones. The route tests assert on what an HTTP caller can observe: the status,
the message, and whether a task row was written.
"""
import pytest

from app.models.agent_task import AgentTask
from app.portal_url import (
    BLOCKED_HOST_PORTAL_URL_MESSAGE,
    CREDENTIALS_PORTAL_URL_MESSAGE,
    EMPTY_PORTAL_URL_MESSAGE,
    NON_HTTPS_PORTAL_URL_MESSAGE,
    UNPARSEABLE_PORTAL_URL_MESSAGE,
    portal_url_error,
)


@pytest.mark.parametrize(
    "url",
    [
        "https://portal.azure.com",
        "https://console.aws.amazon.com/iam/home#/users",
        "https://github.com/orgs/wso2/settings",
        "https://portal.example.com/login",
        "https://Portal.Azure.com:443/",
        "https://8.8.8.8/",
        "https://134744072/",  # 8.8.8.8, as a browser reads it
        "https://[2606:4700:4700::1111]/",
        "https://xn--mnchen-3ya.de/",  # münchen.de, as a browser sends it
        "  https://portal.azure.com  ",
    ],
)
def test_accepts_a_public_https_page(url):
    assert portal_url_error(url) is None


@pytest.mark.parametrize(
    ("url", "message"),
    [
        ("", EMPTY_PORTAL_URL_MESSAGE),
        ("   ", EMPTY_PORTAL_URL_MESSAGE),
        ("portal.azure.com", UNPARSEABLE_PORTAL_URL_MESSAGE),
        ("http://portal.azure.com", NON_HTTPS_PORTAL_URL_MESSAGE),
        ("javascript:alert(1)", NON_HTTPS_PORTAL_URL_MESSAGE),
        ("file:///etc/passwd", NON_HTTPS_PORTAL_URL_MESSAGE),
        ("https://user:pass@example.com", CREDENTIALS_PORTAL_URL_MESSAGE),
        ("https://@example.com", CREDENTIALS_PORTAL_URL_MESSAGE),
    ],
)
def test_refuses_a_non_https_or_credentialed_url(url, message):
    assert portal_url_error(url) == message


@pytest.mark.parametrize(
    "url",
    [
        "https://localhost",
        "https://LOCALHOST./",
        "https://app.localhost",
        "https://metadata.google.internal/computeMetadata/v1/",
        "https://instance-data",
        "https://0.0.0.0",
        "https://10.1.2.3",
        "https://100.64.0.1",
        "https://127.0.0.1:8080",
        "https://169.254.169.254/latest/meta-data",
        "https://172.20.0.1",
        "https://192.0.0.8",
        "https://192.168.1.1",
        "https://198.18.0.1",
        # Shorthand forms a browser reads as 127.0.0.1 or 169.254.169.254.
        "https://127.1",
        "https://0x7f.1",
        "https://0x7f000001",
        "https://2130706433",
        "https://0177.0.0.1",
        "https://169.254.43518",
        "https://0x",  # 0.0.0.0
        # IPv6, and IPv6 forms that carry a blocked IPv4 address.
        "https://[::]",
        "https://[::1]",
        "https://[::127.0.0.1]",
        "https://[::ffff:127.0.0.1]",
        "https://[::ffff:a9fe:a9fe]",
        "https://[64:ff9b::a9fe:a9fe]",
        "https://[64:ff9b:1::1]",
        "https://[2002:7f00:1::]",
        "https://[fd00::1]",
        "https://[fe80::1]",
    ],
)
def test_refuses_a_local_private_or_metadata_host(url):
    assert portal_url_error(url) == BLOCKED_HOST_PORTAL_URL_MESSAGE


@pytest.mark.parametrize(
    "url",
    [
        "not a url",
        "https://example.com\\@127.0.0.1",
        "https://exa\tmple.com",
        "https://%31%32%37.0.0.1",
        "https://１２７.０.０.１",
        "https://münchen.de",
        "https://256.0.0.1",
        "https://1.2.3.4.5",
        "https://08.0.0.1",
        "https://0x0x7f.1",
        "https://example.com:99999",
        "https://[::1",
    ],
)
def test_refuses_what_a_browser_would_rewrite_or_reject(url):
    assert portal_url_error(url) == UNPARSEABLE_PORTAL_URL_MESSAGE


def _task_count(db_session) -> int:
    return db_session.query(AgentTask).count()


def test_login_task_with_a_blocked_url_is_refused_and_not_stored(db_session, engineer_client):
    before = _task_count(db_session)

    response = engineer_client.post(
        "/api/agent/tasks", json={"prompt": "https://169.254.169.254/", "kind": "login"}
    )

    assert response.status_code == 400
    assert response.json()["detail"] == BLOCKED_HOST_PORTAL_URL_MESSAGE
    assert _task_count(db_session) == before


def test_login_task_with_a_public_https_url_is_queued(db_session, engineer_client):
    response = engineer_client.post(
        "/api/agent/tasks", json={"prompt": "https://portal.azure.com/", "kind": "login"}
    )

    assert response.status_code == 200
    assert response.json()["prompt"] == "https://portal.azure.com/"
    assert response.json()["status"] == "queued"


def test_run_task_with_a_blocked_portal_url_is_refused_and_not_stored(db_session, engineer_client):
    before = _task_count(db_session)

    response = engineer_client.post(
        "/api/agent/tasks",
        json={"prompt": "capture the users page", "portal_url": "http://portal.azure.com"},
    )

    assert response.status_code == 400
    assert response.json()["detail"] == NON_HTTPS_PORTAL_URL_MESSAGE
    assert _task_count(db_session) == before


def test_run_task_keeps_a_free_text_prompt_and_an_empty_portal_url(engineer_client):
    """A run's prompt is instructions, not a URL, and an empty portal_url means
    none was picked -- neither is checked."""
    response = engineer_client.post(
        "/api/agent/tasks",
        json={"prompt": "open the users page and capture it", "portal_url": ""},
    )

    assert response.status_code == 200
    assert response.json()["kind"] == "run"


def test_reset_task_is_queued_without_a_url(engineer_client):
    response = engineer_client.post("/api/agent/tasks", json={"prompt": "reset", "kind": "reset"})

    assert response.status_code == 200
    assert response.json()["kind"] == "reset"


def test_unknown_task_kind_is_refused_and_not_stored(db_session, engineer_client):
    before = _task_count(db_session)

    response = engineer_client.post(
        "/api/agent/tasks", json={"prompt": "https://127.0.0.1/", "kind": "Login"}
    )

    assert response.status_code == 400
    assert response.json()["detail"] == "Kind must be one of: login, reset, run"
    assert _task_count(db_session) == before
