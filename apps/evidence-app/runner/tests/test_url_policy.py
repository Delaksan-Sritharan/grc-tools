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

"""Unit tests for `wso2_runner.url_policy` — the policy the browser enforces.

Two things are pinned here. The policy is read as the backend sends it, with
only its shape checked -- and an empty allowlist refused, because the browser
library reads that as "allow every page". And the fixed address floor: which
resolved addresses a connection may reach, including the IPv6 forms that
carry an IPv4 address, and the exceptions for private addresses, through an
internal host name or inside an internal range. No policy entry can open
loopback, link-local or metadata addresses.
"""
import pytest

from wso2_runner.url_policy import PolicyError, RunnerUrlPolicy, address_class, version_tuple


def _policy(**overrides):
    data = {"allowed_domains": ["*.azure.com", "github.com"], "internal_hosts": [], "min_runner_version": None}
    data.update(overrides)
    return RunnerUrlPolicy.from_json(data)


# --- Reading the policy --------------------------------------------------


def test_a_policy_is_read_into_names_and_networks():
    policy = _policy(internal_hosts=["Jira.Internal", "10.20.30.40", "10.30.0.0/16", "fd00::5"])

    assert policy.allowed_domains == ("*.azure.com", "github.com")
    assert policy.internal_names == frozenset({"jira.internal"})
    assert [str(n) for n in policy.internal_networks] == ["10.20.30.40/32", "10.30.0.0/16", "fd00::5/128"]


def test_the_browser_allowlist_has_domains_names_and_single_addresses_but_not_ranges():
    policy = _policy(internal_hosts=["jira.internal", "10.20.30.40", "10.30.0.0/16"])

    assert policy.browser_allowed_domains() == ["*.azure.com", "github.com", "jira.internal", "10.20.30.40"]


@pytest.mark.parametrize(
    "overrides",
    [
        {"allowed_domains": []},
        {"allowed_domains": "*.azure.com"},
        {"allowed_domains": [""]},
        {"allowed_domains": [5]},
        {"internal_hosts": "jira.internal"},
        {"internal_hosts": [None]},
        {"min_runner_version": 1.1},
    ],
)
def test_a_policy_that_is_not_well_formed_is_refused(overrides):
    with pytest.raises(PolicyError):
        _policy(**overrides)


def test_a_response_that_is_not_a_policy_is_refused():
    with pytest.raises(PolicyError):
        RunnerUrlPolicy.from_json(["*.azure.com"])


# --- The address floor ---------------------------------------------------


@pytest.mark.parametrize(
    ("address", "expected"),
    [
        ("127.0.0.1", "blocked"),
        ("127.8.9.10", "blocked"),
        ("0.0.0.0", "blocked"),
        ("169.254.169.254", "blocked"),
        ("224.0.0.1", "blocked"),
        ("255.255.255.255", "blocked"),
        ("::1", "blocked"),
        ("::", "blocked"),
        ("fe80::1%en0", "blocked"),
        ("ff02::1", "blocked"),
        ("::ffff:127.0.0.1", "blocked"),
        ("64:ff9b::a9fe:a9fe", "blocked"),
        ("2002:7f00:1::", "blocked"),
        ("64:ff9b:1::1", "blocked"),
        ("10.1.2.3", "private"),
        ("172.20.0.1", "private"),
        ("192.168.1.1", "private"),
        ("100.64.0.1", "private"),
        ("fd00::1", "private"),
        ("::ffff:10.0.0.5", "private"),
        ("8.8.8.8", "public"),
        ("2606:4700:4700::1111", "public"),
        ("::ffff:8.8.8.8", "public"),
    ],
)
def test_address_class(address, expected):
    assert address_class(address) == expected


def test_a_public_address_may_be_reached_from_any_host():
    assert _policy().connection_refusal("portal.azure.com", "20.50.2.1") is None


def test_a_name_that_resolves_to_loopback_is_refused():
    reason = _policy().connection_refusal("127.0.0.1.nip.io", "127.0.0.1")

    assert reason is not None and "loopback" in reason


def test_a_private_address_is_refused_unless_the_host_is_an_internal_name():
    policy = _policy(internal_hosts=["jira.internal"])

    assert policy.connection_refusal("evil.example", "10.0.0.5") is not None
    assert policy.connection_refusal("jira.internal", "10.0.0.5") is None
    assert policy.connection_refusal("JIRA.internal.", "10.0.0.5") is None


def test_a_private_address_inside_an_internal_range_may_be_reached():
    policy = _policy(internal_hosts=["10.20.0.0/16", "fd00:20::/32"])

    assert policy.connection_refusal("build.internal.example", "10.20.5.7") is None
    assert policy.connection_refusal("10.20.5.7", "10.20.5.7") is None
    assert policy.connection_refusal("ci.internal.example", "fd00:20::9") is None
    assert policy.connection_refusal("ci.internal.example", "::ffff:10.20.5.7") is None
    assert policy.connection_refusal("build.internal.example", "10.21.0.1") is not None


def test_no_internal_entry_opens_loopback_link_local_or_metadata():
    policy = _policy(internal_hosts=["jira.internal", "127.0.0.0/8", "169.254.0.0/16", "0.0.0.0/0"])

    assert policy.connection_refusal("jira.internal", "127.0.0.1") is not None
    assert policy.connection_refusal("anything.example", "127.0.0.1") is not None
    assert policy.connection_refusal("anything.example", "169.254.169.254") is not None


# --- Versions ------------------------------------------------------------


@pytest.mark.parametrize(
    ("value", "expected"),
    [("1.1.0", (1, 1, 0)), ("1.1", (1, 1, 0)), ("2", (2, 0, 0)), ("1.2.3rc1", (1, 2, 3)), ("0.0.0-unknown", (0, 0, 0)), ("dev", (0, 0, 0))],
)
def test_version_tuple(value, expected):
    assert version_tuple(value) == expected


def test_requires_newer_than_compares_against_the_minimum():
    policy = _policy(min_runner_version="1.1.0")

    assert policy.requires_newer_than("1.0.9") is True
    assert policy.requires_newer_than("0.0.0-unknown") is True
    assert policy.requires_newer_than("1.1.0") is False
    assert policy.requires_newer_than("1.2.0") is False
    assert _policy().requires_newer_than("0.0.1") is False
