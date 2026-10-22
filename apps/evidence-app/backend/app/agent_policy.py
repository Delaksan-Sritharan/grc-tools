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

"""Where an Agent Task's browser may go, and which Runners may take tasks.

The policy is configuration, set for each environment by the people who
deploy this service, so a portal can be approved without a code change or a
Runner release:

- `AGENT_ALLOWED_DOMAINS` (required): the domains the Runner's browser may
  open pages on. `example.com` is that host, plus `www.example.com` for a
  two-label domain; `*.example.com` is the domain and every subdomain. These
  are the patterns the Runner's browser library understands, so both sides
  read an entry the same way.
- `AGENT_INTERNAL_HOSTS`: what may reach private network addresses. A host
  name or a single address is also approved as a page. A range (CIDR, e.g.
  10.20.0.0/16) lets connections reach private addresses inside it, but is not
  a page approval: the browser can't match pages against a range, so a page
  opened by a raw address needs that address listed on its own.
- `AGENT_MIN_RUNNER_VERSION`: Runners older than this get no tasks.

The policy is used in two places. `create_task` refuses a login URL or portal
URL whose host is not approved, and `GET /agent/policy` hands the policy to
each Runner, which enforces it in its own browser. The Runner is the
boundary: it also refuses loopback, link-local and cloud metadata addresses
whatever is configured, and checks the address every name actually resolves
to. So the checks here are for clear errors and catching typos, not for
recognising every way an address can be disguised -- anything not on the
list is simply not approved.
"""
import ipaddress
import re
from dataclasses import dataclass
from urllib.parse import urlsplit

EMPTY_PORTAL_URL_MESSAGE = "Please enter a URL"
UNPARSEABLE_PORTAL_URL_MESSAGE = "That is not a valid URL. Use the form https://..."
NON_HTTPS_PORTAL_URL_MESSAGE = "Only https:// URLs are allowed."
CREDENTIALS_PORTAL_URL_MESSAGE = "Remove the username or password from the URL."
NOT_APPROVED_PORTAL_URL_MESSAGE = (
    "That portal isn't on the approved list. Ask an Evidence Portal admin to add it."
)

# The Runner's browser library stops matching patterns once its page list
# reaches 100 entries (it switches to exact host lookups), so the page list --
# allowed domains plus the internal hosts and addresses -- stays below that.
MAX_PAGE_ENTRIES = 99

_LABEL = r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?"
_DOMAIN_RE = re.compile(rf"^{_LABEL}(?:\.{_LABEL})+$")
_HOST_RE = re.compile(rf"^{_LABEL}(?:\.{_LABEL})*$")
_VERSION_RE = re.compile(r"^\d+(?:\.\d+){0,2}$")
_USER_AGENT_VERSION_RE = re.compile(r"wso2-runner/(\d+(?:\.\d+){0,2})")

IpAddress = ipaddress.IPv4Address | ipaddress.IPv6Address


@dataclass(frozen=True)
class AgentPolicy:
    allowed_domains: tuple[str, ...]
    # As configured, normalised: host names, single addresses and ranges.
    internal_hosts: tuple[str, ...]
    min_runner_version: str | None

    @property
    def internal_names(self) -> frozenset[str]:
        return frozenset(entry for entry in self.internal_hosts if not _is_address_entry(entry))

    @property
    def internal_addresses(self) -> frozenset[IpAddress]:
        return frozenset(
            ipaddress.ip_address(entry) for entry in self.internal_hosts if _is_address_entry(entry) and "/" not in entry
        )


def _entries(raw: str) -> list[str]:
    return [entry.strip().lower() for entry in raw.split(",") if entry.strip()]


def _is_address_entry(entry: str) -> bool:
    try:
        ipaddress.ip_network(entry, strict=False)
        return True
    except ValueError:
        return False


def parse_allowed_domains(raw: str) -> tuple[str, ...]:
    entries = _entries(raw)
    if not entries:
        raise ValueError("AGENT_ALLOWED_DOMAINS approves no portals at all.")
    for entry in entries:
        domain = entry[2:] if entry.startswith("*.") else entry
        # At least two labels, and not an address: "*" or "*.com" would approve
        # every site, and an address belongs in AGENT_INTERNAL_HOSTS.
        if not _DOMAIN_RE.match(domain) or domain.rsplit(".", 1)[-1].isdigit():
            raise ValueError(
                f'AGENT_ALLOWED_DOMAINS entry "{entry}" is not a domain. Use example.com or *.example.com.'
            )
    return tuple(entries)


def parse_internal_hosts(raw: str) -> tuple[str, ...]:
    hosts = []
    for entry in _entries(raw):
        try:
            network = ipaddress.ip_network(entry, strict=False)
        except ValueError:
            network = None
        if network is not None:
            # One address is written as itself; a range as its CIDR.
            normalised = str(network.network_address) if network.num_addresses == 1 else str(network)
        elif _HOST_RE.match(entry) and not entry.rsplit(".", 1)[-1].isdigit():
            normalised = entry
        else:
            raise ValueError(
                f'AGENT_INTERNAL_HOSTS entry "{entry}" must be a host name, an address, or a range such as 10.20.0.0/16.'
            )
        if normalised not in hosts:
            hosts.append(normalised)
    return tuple(hosts)


def parse_min_runner_version(raw: str) -> str | None:
    value = raw.strip()
    if not value:
        return None
    if not _VERSION_RE.match(value):
        raise ValueError(f'AGENT_MIN_RUNNER_VERSION "{value}" is not a version such as 1.1.0.')
    return value


def check_page_entry_count(policy: AgentPolicy) -> None:
    count = len(policy.allowed_domains) + len(policy.internal_names) + len(policy.internal_addresses)
    if count > MAX_PAGE_ENTRIES:
        raise ValueError(
            f"AGENT_ALLOWED_DOMAINS and the hosts and addresses in AGENT_INTERNAL_HOSTS add up to {count} "
            f"entries; the most the Runner's browser can match is {MAX_PAGE_ENTRIES}. Use *.example.com "
            "patterns, or ranges, to shorten the list."
        )


def build_policy(allowed_domains: str, internal_hosts: str, min_runner_version: str) -> AgentPolicy:
    policy = AgentPolicy(
        allowed_domains=parse_allowed_domains(allowed_domains),
        internal_hosts=parse_internal_hosts(internal_hosts),
        min_runner_version=parse_min_runner_version(min_runner_version),
    )
    check_page_entry_count(policy)
    return policy


def current_policy() -> AgentPolicy:
    # Imported here so app.config can use build_policy to validate its own
    # settings without the two modules importing each other.
    from app.config import settings

    return build_policy(settings.AGENT_ALLOWED_DOMAINS, settings.AGENT_INTERNAL_HOSTS, settings.AGENT_MIN_RUNNER_VERSION)


def host_is_approved(host: str, policy: AgentPolicy) -> bool:
    """Whether a page on `host` may be opened: it matches an allowed domain,
    or is an internal host name or single internal address."""
    host = host.lower().rstrip(".")
    try:
        return ipaddress.ip_address(host) in policy.internal_addresses
    except ValueError:
        pass
    if host in policy.internal_names:
        return True
    for pattern in policy.allowed_domains:
        if pattern.startswith("*."):
            domain = pattern[2:]
            if host == domain or host.endswith("." + domain):
                return True
        elif host == pattern or (pattern.count(".") == 1 and host == "www." + pattern):
            return True
    return False


def portal_url_error(value: str, policy: AgentPolicy) -> str | None:
    """Why a login URL or portal URL is refused, as a message to show the user,
    or None when it may be queued."""
    url = value.strip()
    if not url:
        return EMPTY_PORTAL_URL_MESSAGE
    # A browser reads a backslash as "/" and drops whitespace, so a URL with
    # them can name a different host from the one read here. They never
    # appear in a real portal URL, so such a URL is refused outright.
    if not url.isascii() or any(c.isspace() or c == "\\" or ord(c) < 0x20 or ord(c) == 0x7F for c in url):
        return UNPARSEABLE_PORTAL_URL_MESSAGE
    try:
        parts = urlsplit(url)
        parts.port  # raises ValueError on a port a browser would refuse
    except ValueError:
        return UNPARSEABLE_PORTAL_URL_MESSAGE
    if not parts.scheme:
        return UNPARSEABLE_PORTAL_URL_MESSAGE
    if parts.scheme != "https":
        return NON_HTTPS_PORTAL_URL_MESSAGE
    if "@" in parts.netloc:
        return CREDENTIALS_PORTAL_URL_MESSAGE
    host = parts.hostname
    if not host:
        return UNPARSEABLE_PORTAL_URL_MESSAGE
    return None if host_is_approved(host, policy) else NOT_APPROVED_PORTAL_URL_MESSAGE


def runner_version(explicit: str | None, user_agent: str | None) -> tuple[int, int, int] | None:
    """The version a Runner reports: its X-Runner-Version header, or, for a
    Runner older than that header, the version in its User-Agent."""
    if explicit and _VERSION_RE.match(explicit.strip()):
        return version_tuple(explicit.strip())
    match = _USER_AGENT_VERSION_RE.search(user_agent or "")
    return version_tuple(match.group(1)) if match else None


def version_tuple(value: str) -> tuple[int, int, int]:
    parts = [int(part) for part in value.split(".")]
    return tuple((parts + [0, 0, 0])[:3])
