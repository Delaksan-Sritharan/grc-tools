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

"""The rules every portal URL on an Agent Task must pass before it is stored.

A `login` task's `prompt` is a URL the Runner opens as-is in a real browser on
the engineer's machine, and a `run` task's `portal_url` names the portal the
agent is told it is on. The Runner trusts whatever the backend hands it, so the
rules are applied here, when the task is created: a public https web page, with
no credentials in it, and no local, private, link-local or cloud metadata host.
Each refusal is a sentence meant to be shown to the user as-is.

The host is read the way a browser reads it, because a browser is what opens
it. `127.1`, `0x7f.1` and `2130706433` are all 127.0.0.1 to Chrome, so a host
that ends in a number is parsed with the WHATWG URL Standard's IPv4 rules
rather than Python's stricter ones. Anything a browser would rewrite in a way
this module does not model -- a backslash, whitespace, a percent-encoded or
non-ASCII host -- is refused rather than guessed at. An internationalised
domain is accepted in its `xn--` form, which is what a browser sends.
"""
import ipaddress
from urllib.parse import urlsplit

EMPTY_PORTAL_URL_MESSAGE = "Please enter a URL"
UNPARSEABLE_PORTAL_URL_MESSAGE = "That is not a valid URL. Use the form https://..."
NON_HTTPS_PORTAL_URL_MESSAGE = "Only https:// URLs are allowed."
CREDENTIALS_PORTAL_URL_MESSAGE = "Remove the username or password from the URL."
BLOCKED_HOST_PORTAL_URL_MESSAGE = "Local and private network addresses are not allowed."

_BLOCKED_HOST_NAMES = frozenset(
    {
        "localhost",
        "metadata",
        "metadata.google.internal",
        "instance-data",
        "instance-data.ec2.internal",
    }
)

_BLOCKED_IPV4_NETWORKS = tuple(
    ipaddress.IPv4Network(network)
    for network in (
        "0.0.0.0/8",  # "this network", including 0.0.0.0
        "10.0.0.0/8",
        "100.64.0.0/10",  # carrier-grade NAT
        "127.0.0.0/8",  # loopback
        "169.254.0.0/16",  # link-local, including 169.254.169.254
        "172.16.0.0/12",
        "192.0.0.0/24",  # IETF protocol assignments
        "192.168.0.0/16",
        "198.18.0.0/15",  # benchmarking
    )
)

# :: and ::1, and the IPv4-compatible form of any address.
_IPV6_FIRST_96_BITS_ZERO = ipaddress.IPv6Network("::/96")
# NAT64 local-use translation maps to whatever the network chooses, so all of
# it is refused.
_IPV6_NAT64_LOCAL = ipaddress.IPv6Network("64:ff9b:1::/48")
# NAT64 well-known prefix, 64:ff9b::a.b.c.d.
_IPV6_NAT64 = ipaddress.IPv6Network("64:ff9b::/96")
_IPV6_BLOCKED_NETWORKS = (
    ipaddress.IPv6Network("fc00::/7"),  # unique local
    ipaddress.IPv6Network("fe80::/10"),  # link-local
)

_DOMAIN_CHARACTERS = frozenset("abcdefghijklmnopqrstuvwxyz0123456789-._")
_DIGITS_BY_RADIX = {
    8: frozenset("01234567"),
    10: frozenset("0123456789"),
    16: frozenset("0123456789abcdefABCDEF"),
}


def _is_blocked_ipv4(address: ipaddress.IPv4Address) -> bool:
    return any(address in network for network in _BLOCKED_IPV4_NETWORKS)


def _is_blocked_ipv6(address: ipaddress.IPv6Address) -> bool:
    if address in _IPV6_FIRST_96_BITS_ZERO or address in _IPV6_NAT64_LOCAL:
        return True
    # The IPv4 address an IPv6 form carries: ::ffff:a.b.c.d, 64:ff9b::a.b.c.d,
    # or 6to4's 2002:aabb:ccdd::/48.
    if address.ipv4_mapped is not None:
        return _is_blocked_ipv4(address.ipv4_mapped)
    if address in _IPV6_NAT64:
        return _is_blocked_ipv4(ipaddress.IPv4Address(int(address) & 0xFFFFFFFF))
    if address.sixtofour is not None:
        return _is_blocked_ipv4(address.sixtofour)
    return any(address in network for network in _IPV6_BLOCKED_NETWORKS)


def _parse_ipv4_number(part: str) -> int | None:
    """The WHATWG URL Standard's IPv4 number parser: decimal, 0x hex, or
    leading-zero octal. None when the part is not a number in its radix."""
    if not part:
        return None
    radix = 10
    if part[:2] in ("0x", "0X"):
        part, radix = part[2:], 16
    elif len(part) > 1 and part[0] == "0":
        part, radix = part[1:], 8
    if not part:
        return 0
    # Checked by hand rather than left to int(), which also accepts "_", a
    # sign, surrounding whitespace and a second 0x/0o prefix -- none of which
    # a browser does.
    if not set(part) <= _DIGITS_BY_RADIX[radix]:
        return None
    return int(part, radix)


def _host_parts(host: str) -> list[str]:
    """The host's dot-separated parts, minus the empty one a single trailing
    dot leaves, as the WHATWG IPv4 parser reads them."""
    parts = host.split(".")
    if len(parts) > 1 and parts[-1] == "":
        parts.pop()
    return parts


def _ends_in_a_number(host: str) -> bool:
    """The WHATWG "ends in a number" check: true when a browser would parse
    the host as an IPv4 address rather than as a domain name."""
    last = _host_parts(host)[-1]
    if last and set(last) <= _DIGITS_BY_RADIX[10]:
        return True
    return _parse_ipv4_number(last) is not None


def _parse_whatwg_ipv4(host: str) -> ipaddress.IPv4Address | None:
    """The address a browser reads from a host that ends in a number, or None
    where the browser would refuse the URL altogether."""
    parts = _host_parts(host)
    if len(parts) > 4:
        return None
    numbers = [_parse_ipv4_number(part) for part in parts]
    if any(number is None for number in numbers):
        return None
    *leading, last = numbers
    if any(number > 255 for number in leading) or last >= 256 ** (5 - len(numbers)):
        return None
    value = last + sum(number * 256 ** (3 - index) for index, number in enumerate(leading))
    return ipaddress.IPv4Address(value)


def portal_url_error(value: str) -> str | None:
    """Why a portal URL is refused, as a message to show the user, or None
    when it may be stored and opened."""
    trimmed = value.strip()
    if not trimmed:
        return EMPTY_PORTAL_URL_MESSAGE
    if not trimmed.isascii() or any(
        char.isspace() or char == "\\" or ord(char) < 0x20 or ord(char) == 0x7F for char in trimmed
    ):
        return UNPARSEABLE_PORTAL_URL_MESSAGE

    try:
        parts = urlsplit(trimmed)
        parts.port  # raises ValueError on a port a browser would refuse
    except ValueError:
        return UNPARSEABLE_PORTAL_URL_MESSAGE
    if not parts.scheme:
        return UNPARSEABLE_PORTAL_URL_MESSAGE
    if parts.scheme != "https":
        return NON_HTTPS_PORTAL_URL_MESSAGE
    if not parts.netloc:
        return UNPARSEABLE_PORTAL_URL_MESSAGE

    if "@" in parts.netloc:
        return CREDENTIALS_PORTAL_URL_MESSAGE

    host = (parts.hostname or "").rstrip(".")
    if not host:
        return BLOCKED_HOST_PORTAL_URL_MESSAGE

    if ":" in host:
        try:
            ipv6 = ipaddress.IPv6Address(host)
        except ValueError:
            return UNPARSEABLE_PORTAL_URL_MESSAGE
        return BLOCKED_HOST_PORTAL_URL_MESSAGE if _is_blocked_ipv6(ipv6) else None

    if _ends_in_a_number(host):
        ipv4 = _parse_whatwg_ipv4(host)
        if ipv4 is None:
            return UNPARSEABLE_PORTAL_URL_MESSAGE
        return BLOCKED_HOST_PORTAL_URL_MESSAGE if _is_blocked_ipv4(ipv4) else None

    if not set(host) <= _DOMAIN_CHARACTERS:
        return UNPARSEABLE_PORTAL_URL_MESSAGE
    if host in _BLOCKED_HOST_NAMES or host.endswith(".localhost"):
        return BLOCKED_HOST_PORTAL_URL_MESSAGE
    return None
