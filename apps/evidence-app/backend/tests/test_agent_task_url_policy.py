# Copyright (c) 2026 WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

"""
`POST /agent/tasks` only queues a portal URL that is on the approved list.

A `login` task's `prompt` is opened by the Runner in a real browser, and a
`run` task's `portal_url` names the portal the agent is told it is on. Each
must be an https URL, with no credentials in it, whose host is an allowed
domain, an internal host name or a single internal address
(`app/agent_policy.py`).

The check is an allowlist, so it doesn't need to recognise disguised
addresses: `127.1`, `2130706433` or `127.0.0.1.nip.io` are refused because
they are not on the list, not because they are spotted as loopback. Where an
approved name actually leads is the Runner's to check, on the address it
resolves to.
"""
import pytest

from app.agent_policy import (
    CREDENTIALS_PORTAL_URL_MESSAGE,
    EMPTY_PORTAL_URL_MESSAGE,
    NON_HTTPS_PORTAL_URL_MESSAGE,
    NOT_APPROVED_PORTAL_URL_MESSAGE,
    UNPARSEABLE_PORTAL_URL_MESSAGE,
    build_policy,
    portal_url_error,
)
from app.models.agent_task import AgentTask

_POLICY = build_policy("*.azure.com,github.com", "jira.internal,10.20.30.40,10.30.0.0/16", "")


@pytest.mark.parametrize(
    "url",
    [
        "https://portal.azure.com",
        "https://ms.portal.azure.com/#home",
        "https://Portal.Azure.com:443/",
        "https://github.com/orgs/wso2/settings",
        "https://www.github.com/",
        "https://jira.internal/browse/SEC-1",
        "https://10.20.30.40/",
        "  https://portal.azure.com  ",
    ],
)
def test_an_approved_https_page_is_accepted(url):
    assert portal_url_error(url, _POLICY) is None


@pytest.mark.parametrize(
    "url",
    [
        "https://evil.example/",
        "https://azure.com.evil.example/",
        "https://gist.github.com/",
        "https://localhost/",
        "https://127.0.0.1/",
        "https://127.1/",
        "https://2130706433/",
        "https://169.254.169.254/latest/meta-data",
        "https://127.0.0.1.nip.io/",
        "https://[::1]/",
        # In an internal range, but a range lets connections through, it
        # does not approve pages: a raw address must be listed on its own.
        "https://10.30.5.7/",
    ],
)
def test_a_page_not_on_the_list_is_refused(url):
    assert portal_url_error(url, _POLICY) == NOT_APPROVED_PORTAL_URL_MESSAGE


@pytest.mark.parametrize(
    ("url", "message"),
    [
        ("", EMPTY_PORTAL_URL_MESSAGE),
        ("   ", EMPTY_PORTAL_URL_MESSAGE),
        ("portal.azure.com", UNPARSEABLE_PORTAL_URL_MESSAGE),
        ("http://portal.azure.com", NON_HTTPS_PORTAL_URL_MESSAGE),
        ("javascript:alert(1)", NON_HTTPS_PORTAL_URL_MESSAGE),
        ("file:///etc/passwd", NON_HTTPS_PORTAL_URL_MESSAGE),
        ("https://user:pass@portal.azure.com", CREDENTIALS_PORTAL_URL_MESSAGE),
        ("https://@portal.azure.com", CREDENTIALS_PORTAL_URL_MESSAGE),
        # A browser reads "\" as "/", so these name evil.example, not azure.com.
        ("https://evil.example\\.azure.com", UNPARSEABLE_PORTAL_URL_MESSAGE),
        ("https://portal.azure.com\\@evil.example", UNPARSEABLE_PORTAL_URL_MESSAGE),
        ("https://exa\tmple.azure.com", UNPARSEABLE_PORTAL_URL_MESSAGE),
        ("https://portal.azure.com:99999/", UNPARSEABLE_PORTAL_URL_MESSAGE),
        ("https://[::1", UNPARSEABLE_PORTAL_URL_MESSAGE),
    ],
)
def test_a_url_that_is_not_plain_https_is_refused_with_a_message_saying_why(url, message):
    assert portal_url_error(url, _POLICY) == message


def _task_count(db_session) -> int:
    return db_session.query(AgentTask).count()


def test_login_task_for_an_unapproved_page_is_refused_and_not_stored(db_session, engineer_client):
    before = _task_count(db_session)

    response = engineer_client.post("/api/agent/tasks", json={"prompt": "https://169.254.169.254/", "kind": "login"})

    assert response.status_code == 400
    assert response.json()["detail"] == NOT_APPROVED_PORTAL_URL_MESSAGE
    assert _task_count(db_session) == before


def test_login_task_for_an_approved_page_is_queued(engineer_client):
    response = engineer_client.post("/api/agent/tasks", json={"prompt": "https://portal.azure.com/", "kind": "login"})

    assert response.status_code == 200
    assert response.json()["prompt"] == "https://portal.azure.com/"
    assert response.json()["status"] == "queued"


def test_run_task_with_a_plain_http_portal_url_is_refused_and_not_stored(db_session, engineer_client):
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

    response = engineer_client.post("/api/agent/tasks", json={"prompt": "https://127.0.0.1/", "kind": "Login"})

    assert response.status_code == 400
    assert response.json()["detail"] == "Kind must be one of: login, reset, run"
    assert _task_count(db_session) == before
