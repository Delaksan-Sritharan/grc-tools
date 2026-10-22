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

"""Where the Runner's browser may go.

Two layers, enforced on this machine:

- **The approved portals**, from the backend (`GET /api/agent/policy`). The
  browser only opens pages on an allowed domain, an internal host name or a
  single internal address. Admins manage the list in the backend; the Runner
  applies it as sent.
- **A fixed floor on network addresses**, enforced for every connection the
  browser makes (pages, redirects, scripts, images) by the local egress proxy
  in `egress_proxy.py`, on the address a name actually resolves to. Loopback,
  link-local, cloud metadata (169.254.169.254, and AWS's IPv6 fd00:ec2::254),
  multicast and reserved addresses are never reachable, whatever the policy
  says. A private address
  is reachable only through an internal host name, or when it lies inside an
  internal range. A range is not a page approval: the browser can't match
  pages against a range, so a page opened by a raw address needs that address
  listed on its own.
"""
import ipaddress
import re
from dataclasses import dataclass

_VERSION_RE = re.compile(r"^(\d+)(?:\.(\d+))?(?:\.(\d+))?")

IpAddress = ipaddress.IPv4Address | ipaddress.IPv6Address
IpNetwork = ipaddress.IPv4Network | ipaddress.IPv6Network

_ALWAYS_BLOCKED = tuple(
    ipaddress.ip_network(network)
    for network in (
        "0.0.0.0/8",  # "this network", including 0.0.0.0
        "127.0.0.0/8",  # loopback
        "169.254.0.0/16",  # link-local, including the 169.254.169.254 metadata address
        "192.0.0.0/24",  # IETF protocol assignments
        "198.18.0.0/15",  # benchmarking
        "224.0.0.0/4",  # multicast
        "240.0.0.0/4",  # reserved, including 255.255.255.255
        "::/96",  # :: and ::1, and the IPv4-compatible form of any address
        "64:ff9b:1::/48",  # NAT64 local use: maps to whatever the network chooses
        "fe80::/10",  # link-local
        "fec0::/10",  # site-local (deprecated)
        "ff00::/8",  # multicast
        "fd00:ec2::254/128",  # AWS EC2 instance metadata over IPv6, inside the unique-local range
    )
)
_PRIVATE = tuple(
    ipaddress.ip_network(network)
    for network in (
        "10.0.0.0/8",
        "100.64.0.0/10",  # carrier-grade NAT
        "172.16.0.0/12",
        "192.168.0.0/16",
        "fc00::/7",  # unique local
    )
)
_NAT64 = ipaddress.IPv6Network("64:ff9b::/96")


class PolicyError(ValueError):
    """The policy the backend sent can't be used. The message says why."""


def _effective_address(address: str) -> IpAddress:
    """The address a connection really goes to: an IPv6 form that carries an
    IPv4 address (::ffff:a.b.c.d, 64:ff9b::a.b.c.d, 6to4) is routed to that
    IPv4 address, so it is judged as one."""
    ip = ipaddress.ip_address(address.split("%", 1)[0])  # drop an IPv6 scope id
    if isinstance(ip, ipaddress.IPv6Address):
        if ip.ipv4_mapped is not None:
            return ip.ipv4_mapped
        if ip in _NAT64:
            return ipaddress.IPv4Address(int(ip) & 0xFFFFFFFF)
        if ip.sixtofour is not None:
            return ip.sixtofour
    return ip


def address_class(address: str) -> str:
    """"blocked", "private" or "public" for one resolved address."""
    ip = _effective_address(address)
    if any(ip in network for network in _ALWAYS_BLOCKED):
        return "blocked"
    if any(ip in network for network in _PRIVATE):
        return "private"
    return "public"


def version_tuple(value: str) -> tuple[int, int, int]:
    """(major, minor, patch) from a version string; (0, 0, 0) when it has none,
    as an unpackaged checkout reports "0.0.0-unknown"."""
    match = _VERSION_RE.match(value.strip())
    if not match:
        return (0, 0, 0)
    return tuple(int(part or 0) for part in match.groups())


def _string_list(data: dict, key: str) -> list[str]:
    value = data.get(key)
    if not isinstance(value, list) or not all(isinstance(item, str) and item.strip() for item in value):
        raise PolicyError(f"{key} is not a list of names.")
    return [item.strip().lower() for item in value]


@dataclass(frozen=True)
class RunnerUrlPolicy:
    allowed_domains: tuple[str, ...]
    internal_names: frozenset[str]
    internal_networks: tuple[IpNetwork, ...]
    min_runner_version: str | None

    @classmethod
    def from_json(cls, data: object) -> "RunnerUrlPolicy":
        """Read the policy as the backend sends it. The backend checks its
        settings; this only checks the shape, and refuses an empty allowlist,
        which the browser library would read as "allow every page"."""
        if not isinstance(data, dict):
            raise PolicyError("the response is not a policy object.")
        allowed = tuple(_string_list(data, "allowed_domains"))
        if not allowed:
            raise PolicyError("it approves no portals at all.")
        names, networks = set(), []
        for entry in _string_list(data, "internal_hosts"):
            try:
                networks.append(ipaddress.ip_network(entry, strict=False))
            except ValueError:
                names.add(entry.rstrip("."))
        minimum = data.get("min_runner_version")
        if minimum is not None and not isinstance(minimum, str):
            raise PolicyError("min_runner_version is not a version.")
        return cls(
            allowed_domains=allowed,
            internal_names=frozenset(names),
            internal_networks=tuple(networks),
            min_runner_version=minimum or None,
        )

    def browser_allowed_domains(self) -> list[str]:
        """The page allowlist for the browser: approved domains, internal host
        names, and single internal addresses. Ranges are left out -- see the
        module docstring."""
        single_addresses = [str(n.network_address) for n in self.internal_networks if n.num_addresses == 1]
        return [*self.allowed_domains, *sorted(self.internal_names), *single_addresses]

    def connection_refusal(self, host: str, address: str) -> str | None:
        """Why a connection to `host`, resolved to `address`, is refused, or
        None when it may go ahead."""
        kind = address_class(address)
        if kind == "blocked":
            return f"{address} is a loopback, link-local, metadata or reserved address"
        if kind == "private":
            ip = _effective_address(address)
            if host.lower().rstrip(".") in self.internal_names:
                return None
            if any(ip.version == network.version and ip in network for network in self.internal_networks):
                return None
            return f"{address} is a private network address outside the approved internal hosts and ranges"
        return None

    def requires_newer_than(self, version: str) -> bool:
        return bool(self.min_runner_version) and version_tuple(version) < version_tuple(self.min_runner_version)
