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

"""The local proxy every connection from the Runner's browser goes through.

The browser is started with this proxy as its only route out (see
`agent.py`), so pages, redirects, scripts, images and WebSockets all arrive
here as CONNECT requests carrying the host name the page asked for. For each
one the proxy:

1. resolves the name itself, to every IPv4 and IPv6 address it has;
2. refuses the connection if any of those addresses is one the URL policy
   forbids (`RunnerUrlPolicy.connection_refusal`), so a name that points at
   127.0.0.1 or the metadata address gets nowhere, however public it looks;
3. connects to an address it checked, never to a fresh lookup, so a name that
   changes its answer between the check and the connection (DNS rebinding)
   can't redirect the connection.

Only CONNECT is served. Plain-http requests are refused: the policy allows
https pages only, and a proxied plain-http request would be the one route
around the tunnel checks above.
"""
import asyncio
import contextlib
import ipaddress
import socket
import time
from collections.abc import Awaitable, Callable
from urllib.parse import urlsplit

from wso2_runner.url_policy import RunnerUrlPolicy

# A host that exists nowhere. The browser is asked to fetch it once at startup
# (agent.py's guard check); seeing it arrive here proves the browser's traffic
# really goes through this proxy, rather than a machine policy overriding it.
GUARD_CHECK_HOST = "guard-check.wso2-runner.invalid"

_HEADER_TIMEOUT = 15
_RESOLVE_TIMEOUT = 10
_CONNECT_TIMEOUT = 15
# A page retries a refused connection, often many times a second; each refusal
# is reported once a minute per target, so the terminal stays readable.
_LOG_INTERVAL = 60
_FORBIDDEN = b"HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
_BAD_GATEWAY = b"HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"

Resolver = Callable[[str, int], Awaitable[list[str]]]
Connector = Callable[[str, int], Awaitable[tuple[asyncio.StreamReader, asyncio.StreamWriter]]]


async def resolve_all(host: str, port: int) -> list[str]:
    """Every address `host` resolves to, IPv4 and IPv6, in resolver order."""
    try:
        return [str(ipaddress.ip_address(host))]
    except ValueError:
        pass
    infos = await asyncio.get_running_loop().getaddrinfo(host, port, type=socket.SOCK_STREAM)
    addresses: list[str] = []
    for *_ignored, sockaddr in infos:
        if sockaddr[0] not in addresses:
            addresses.append(sockaddr[0])
    return addresses


def _split_target(target: str) -> tuple[str, int] | None:
    """("host", port) from a CONNECT target: "host:443" or "[::1]:443"."""
    if target.startswith("["):
        end = target.find("]")
        if end == -1 or target[end + 1 : end + 2] != ":":
            return None
        host, port_text = target[1:end], target[end + 2 :]
    else:
        host, _sep, port_text = target.rpartition(":")
    if not host or not port_text.isdigit() or not 0 < int(port_text) < 65536:
        return None
    return host.lower().rstrip("."), int(port_text)


class EgressProxy:
    def __init__(
        self,
        policy: RunnerUrlPolicy,
        resolver: Resolver = resolve_all,
        connector: Connector = asyncio.open_connection,
    ):
        self._policy = policy
        self._resolve = resolver
        self._connect = connector
        self._server: asyncio.Server | None = None
        self.port = 0
        self.guard_check_seen = False
        self._last_logged: dict[str, float] = {}

    def set_policy(self, policy: RunnerUrlPolicy) -> None:
        self._policy = policy

    async def start(self) -> int:
        self._server = await asyncio.start_server(self._handle, "127.0.0.1", 0)
        self.port = self._server.sockets[0].getsockname()[1]
        return self.port

    def _log_refusal(self, target: str, reason: str) -> None:
        now = time.monotonic()
        if now - self._last_logged.get(target, -_LOG_INTERVAL) >= _LOG_INTERVAL:
            self._last_logged[target] = now
            print(f"[guard] Blocked {target} — {reason}")

    async def close(self) -> None:
        if self._server is not None:
            self._server.close()
            with contextlib.suppress(Exception):
                await self._server.wait_closed()
            self._server = None

    async def _handle(self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        upstream_writer = None
        try:
            try:
                head = await asyncio.wait_for(reader.readuntil(b"\r\n\r\n"), _HEADER_TIMEOUT)
            except (asyncio.IncompleteReadError, asyncio.LimitOverrunError, asyncio.TimeoutError):
                return
            request_line = head.split(b"\r\n", 1)[0].decode("latin-1").split()
            if len(request_line) != 3 or request_line[0].upper() != "CONNECT":
                if len(request_line) == 3:
                    # Host and port only: a full URL can carry tokens in its query.
                    with contextlib.suppress(ValueError):
                        self._log_refusal(f"http://{urlsplit(request_line[1]).netloc}", "only https is allowed")
                writer.write(_FORBIDDEN)
                return
            target = _split_target(request_line[1])
            if target is None:
                writer.write(_FORBIDDEN)
                return
            host, port = target

            if host == GUARD_CHECK_HOST:
                self.guard_check_seen = True
                writer.write(_FORBIDDEN)
                return

            policy = self._policy
            try:
                addresses = await asyncio.wait_for(self._resolve(host, port), _RESOLVE_TIMEOUT)
            except (OSError, asyncio.TimeoutError):
                writer.write(_BAD_GATEWAY)
                return
            refusals = [r for r in (policy.connection_refusal(host, a) for a in addresses) if r]
            if not addresses or refusals:
                self._log_refusal(f"{host}:{port}", refusals[0] if refusals else "it has no address")
                writer.write(_FORBIDDEN)
                return

            upstream_reader = None
            for address in addresses:
                try:
                    upstream_reader, upstream_writer = await asyncio.wait_for(
                        self._connect(address, port), _CONNECT_TIMEOUT
                    )
                    break
                except (OSError, asyncio.TimeoutError):
                    continue
            if upstream_reader is None:
                writer.write(_BAD_GATEWAY)
                return

            writer.write(b"HTTP/1.1 200 Connection Established\r\n\r\n")
            await writer.drain()
            await asyncio.gather(
                _pipe(reader, upstream_writer),
                _pipe(upstream_reader, writer),
            )
        except (ConnectionError, OSError):
            pass
        finally:
            for stream in (writer, upstream_writer):
                if stream is not None:
                    with contextlib.suppress(Exception):
                        stream.close()


async def _pipe(source: asyncio.StreamReader, sink: asyncio.StreamWriter) -> None:
    try:
        while data := await source.read(65536):
            sink.write(data)
            await sink.drain()
    except (ConnectionError, OSError):
        pass
    finally:
        with contextlib.suppress(Exception):
            sink.close()
