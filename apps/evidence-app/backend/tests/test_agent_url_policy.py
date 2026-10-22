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
The Agent URL policy settings (`app/agent_policy.py`): the approved domains,
the internal hosts, addresses and ranges, and the oldest Runner that may take
tasks.

The Runner fetches the policy from `GET /agent/policy` and enforces it in its
own browser. These tests cover the backend's side: the settings are checked
at startup (format only -- the Runner refuses loopback and metadata addresses
whatever is configured), the endpoint serves them to signed-in callers only,
internal entries reach `create_task` correctly, and `/agent/tasks/next` hands
nothing to a Runner older than the configured minimum. Which URLs a task may
name is covered in test_agent_task_url_policy.py.
"""
import pytest
from pydantic import ValidationError

from app.agent_policy import (
    MAX_PAGE_ENTRIES,
    NOT_APPROVED_PORTAL_URL_MESSAGE,
    build_policy,
    host_is_approved,
    parse_allowed_domains,
    parse_internal_hosts,
    parse_min_runner_version,
    runner_version,
)
from app.api.routes import agent as agent_routes
from app.config import Settings, settings
from app.models.agent_task import AgentTask


# --- Settings ------------------------------------------------------------


@pytest.mark.parametrize(
    "raw",
    ["", ",", "*", "*.com", "com", "*.", "https://portal.azure.com", "portal.azure.com/path", "10.1.2.3", "*.exa mple.com"],
)
def test_an_allowed_domain_that_would_approve_everything_or_is_not_a_domain_is_refused(raw):
    with pytest.raises(ValueError):
        parse_allowed_domains(raw)


def test_allowed_domains_are_trimmed_and_lowercased():
    assert parse_allowed_domains(" *.Azure.com , github.com ,, ") == ("*.azure.com", "github.com")


@pytest.mark.parametrize("raw", ["*.internal.example", "a b", "10.20.30", "http://jira", "jira_internal", "10.20.0.0/40"])
def test_an_internal_entry_that_is_not_a_host_address_or_range_is_refused(raw):
    with pytest.raises(ValueError):
        parse_internal_hosts(raw)


def test_internal_entries_accept_names_addresses_and_ranges():
    assert parse_internal_hosts(
        "Jenkins.Internal.Example, jira, 10.20.30.40, 10.20.30.40/32, 10.30.0.1/16, fd00::5, jira"
    ) == ("jenkins.internal.example", "jira", "10.20.30.40", "10.30.0.0/16", "fd00::5")


@pytest.mark.parametrize("raw", ["latest", "1.x", "v1.1.0", "1.1.0.1"])
def test_a_minimum_runner_version_that_is_not_a_version_is_refused(raw):
    with pytest.raises(ValueError):
        parse_min_runner_version(raw)


def test_an_empty_minimum_runner_version_means_no_minimum():
    assert parse_min_runner_version("  ") is None
    assert parse_min_runner_version("1.1") == "1.1"


def test_a_page_list_longer_than_the_runners_browser_can_match_is_refused():
    domains = ",".join(f"portal{n}.example.com" for n in range(MAX_PAGE_ENTRIES))
    build_policy(domains, "", "")  # exactly at the limit
    with pytest.raises(ValueError, match="add up to"):
        build_policy(domains, "jira", "")
    # Ranges are not page entries, so they don't count.
    build_policy(domains, "10.20.0.0/16,10.30.0.0/16", "")


def test_the_service_refuses_to_start_without_an_allowed_domains_list(monkeypatch):
    """There is no built-in list: each environment must set its own."""
    monkeypatch.delenv("AGENT_ALLOWED_DOMAINS", raising=False)
    with pytest.raises(ValidationError, match="AGENT_ALLOWED_DOMAINS"):
        Settings(_env_file=None)
    with pytest.raises(ValidationError, match="approves no portals"):
        Settings(_env_file=None, AGENT_ALLOWED_DOMAINS="  ")


def test_the_service_refuses_to_start_on_a_malformed_policy():
    with pytest.raises(ValidationError):
        Settings(AGENT_ALLOWED_DOMAINS="*")
    with pytest.raises(ValidationError):
        Settings(AGENT_INTERNAL_HOSTS="*.internal")
    with pytest.raises(ValidationError):
        Settings(AGENT_MIN_RUNNER_VERSION="latest")


# --- Matching ------------------------------------------------------------


_POLICY = build_policy("*.azure.com,github.com,example.co.uk", "jenkins.internal.example,10.20.30.40,10.30.0.0/16", "")


@pytest.mark.parametrize(
    ("host", "approved"),
    [
        ("portal.azure.com", True),
        ("azure.com", True),
        ("ms.portal.azure.com", True),
        ("notazure.com", False),
        ("azure.com.evil.example", False),
        ("github.com", True),
        ("www.github.com", True),
        ("gist.github.com", False),
        ("example.co.uk", True),
        ("www.example.co.uk", False),
        ("jenkins.internal.example", True),
        ("other.internal.example", False),
        ("10.20.30.40", True),
        ("10.20.30.41", False),
        ("10.30.5.7", False),  # in a range: connections may reach it, pages may not
    ],
)
def test_a_page_is_approved_by_domain_pattern_internal_name_or_listed_address(host, approved):
    assert host_is_approved(host, _POLICY) is approved


@pytest.mark.parametrize(
    ("explicit", "user_agent", "expected"),
    [
        ("1.1.0", "wso2-runner/1.0.0 curl/8.5.0", (1, 1, 0)),
        (None, "wso2-runner/1.0.0 curl/8.5.0", (1, 0, 0)),
        ("not a version", "wso2-runner/1.2 curl/8.5.0", (1, 2, 0)),
        (None, "curl/8.5.0", None),
        (None, None, None),
    ],
)
def test_runner_version_prefers_the_header_and_falls_back_to_the_user_agent(explicit, user_agent, expected):
    assert runner_version(explicit, user_agent) == expected


# --- GET /agent/policy ---------------------------------------------------


def test_policy_needs_a_signed_in_caller(client):
    assert client.get("/api/agent/policy").status_code == 401


def test_policy_serves_the_configured_values(monkeypatch, engineer_client):
    monkeypatch.setattr(settings, "AGENT_ALLOWED_DOMAINS", "*.azure.com,github.com")
    monkeypatch.setattr(settings, "AGENT_INTERNAL_HOSTS", "jira,10.20.30.40,10.30.0.1/16")
    monkeypatch.setattr(settings, "AGENT_MIN_RUNNER_VERSION", "1.1.0")

    response = engineer_client.get("/api/agent/policy")

    assert response.status_code == 200
    assert response.json() == {
        "allowed_domains": ["*.azure.com", "github.com"],
        "internal_hosts": ["jira", "10.20.30.40", "10.30.0.0/16"],
        "min_runner_version": "1.1.0",
    }


# --- POST /agent/tasks with internal entries -----------------------------


def test_an_internal_host_is_queued_by_name_and_by_listed_address(monkeypatch, engineer_client):
    monkeypatch.setattr(settings, "AGENT_INTERNAL_HOSTS", "jenkins.internal.example,10.20.30.40")

    by_name = engineer_client.post(
        "/api/agent/tasks", json={"prompt": "https://jenkins.internal.example/", "kind": "login"}
    )
    by_address = engineer_client.post("/api/agent/tasks", json={"prompt": "https://10.20.30.40/", "kind": "login"})

    assert by_name.status_code == 200
    assert by_address.status_code == 200


def test_an_address_that_is_only_in_a_range_is_not_a_page_to_queue(monkeypatch, db_session, engineer_client):
    monkeypatch.setattr(settings, "AGENT_INTERNAL_HOSTS", "10.30.0.0/16")
    before = db_session.query(AgentTask).count()

    response = engineer_client.post("/api/agent/tasks", json={"prompt": "https://10.30.5.7/", "kind": "login"})

    assert response.status_code == 400
    assert response.json()["detail"] == NOT_APPROVED_PORTAL_URL_MESSAGE
    assert db_session.query(AgentTask).count() == before


# --- GET /agent/tasks/next -----------------------------------------------


@pytest.fixture()
def no_polls_recorded(monkeypatch):
    """The poll record is module state shared across tests; start each of
    these with none, so `runner-status` reflects only this test's polls."""
    monkeypatch.setattr(agent_routes, "_last_poll", {})


def _next_task(engineer_client, headers):
    return engineer_client.get("/api/agent/tasks/next", params={"runner_id": "laptop-1"}, headers=headers)


def test_without_a_minimum_any_runner_may_poll(monkeypatch, engineer_client, no_polls_recorded):
    monkeypatch.setattr(settings, "AGENT_MIN_RUNNER_VERSION", "")

    response = _next_task(engineer_client, {"User-Agent": "wso2-runner/1.0.0 curl/8.5.0"})

    assert response.status_code == 200


def test_a_runner_older_than_the_minimum_gets_no_task_and_is_not_shown_online(
    monkeypatch, db_session, engineer_client, no_polls_recorded
):
    monkeypatch.setattr(settings, "AGENT_MIN_RUNNER_VERSION", "1.1.0")
    queued = engineer_client.post("/api/agent/tasks", json={"prompt": "capture the users page"}).json()

    response = _next_task(engineer_client, {"User-Agent": "wso2-runner/1.0.0 curl/8.5.0"})

    assert response.status_code == 426
    assert "1.1.0" in response.json()["detail"]
    assert db_session.get(AgentTask, queued["id"]).status == "queued"
    assert engineer_client.get("/api/agent/runner-status").json()["online"] is False


def test_a_runner_with_no_version_is_refused_once_a_minimum_is_set(monkeypatch, engineer_client, no_polls_recorded):
    monkeypatch.setattr(settings, "AGENT_MIN_RUNNER_VERSION", "1.1.0")

    response = _next_task(engineer_client, {"User-Agent": "curl/8.5.0"})

    assert response.status_code == 426


def test_a_runner_at_the_minimum_polls_normally(monkeypatch, engineer_client, no_polls_recorded):
    monkeypatch.setattr(settings, "AGENT_MIN_RUNNER_VERSION", "1.1.0")

    response = _next_task(
        engineer_client, {"X-Runner-Version": "1.1.0", "User-Agent": "wso2-runner/1.1.0 curl/8.5.0"}
    )

    assert response.status_code == 200
    assert engineer_client.get("/api/agent/runner-status").json()["online"] is True
