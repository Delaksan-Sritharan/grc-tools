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

"""Unit tests for `wso2_runner.egress_proxy` — the proxy the browser must use.

The proxy runs for real on 127.0.0.1, and each test talks to it the way the
browser does: a CONNECT request naming a host. Name resolution is replaced by
a fake resolver, so a test can make any name "resolve" to any address. Where
a tunnel must actually open, a local echo server stands in for the portal and
the test's policy lets 127.0.0.1 through (the real policy never would).

What is pinned: a name is refused when any address it resolves to is
forbidden; the connection goes to the address that was checked, not to a
fresh lookup; plain-http requests are refused; and the browser's startup
check host is recorded without being resolved.
"""
import asyncio

from wso2_runner.egress_proxy import GUARD_CHECK_HOST, EgressProxy
from wso2_runner.url_policy import RunnerUrlPolicy

_REAL_POLICY = RunnerUrlPolicy.from_json(
    {"allowed_domains": ["*.azure.com"], "internal_hosts": ["jira.internal"], "min_runner_version": None}
)


class _LetEverythingThrough:
    """A policy that refuses nothing, so a tunnel to the local echo server can
    open. Used only to test the tunnel itself."""

    def connection_refusal(self, host, address):
        return None


def _fake_resolver(answers, calls):
    async def resolve(host, port):
        calls.append(host)
        return list(answers.get(host, []))

    return resolve


async def _connect(proxy_port, target):
    reader, writer = await asyncio.open_connection("127.0.0.1", proxy_port)
    writer.write(f"CONNECT {target} HTTP/1.1\r\nHost: {target}\r\n\r\n".encode())
    await writer.drain()
    status = (await reader.readline()).decode()
    return status, reader, writer


def _run(coro):
    return asyncio.run(coro)


def test_a_name_resolving_to_loopback_is_refused():
    async def scenario():
        calls = []
        proxy = EgressProxy(_REAL_POLICY, _fake_resolver({"127.0.0.1.nip.io": ["127.0.0.1"]}, calls))
        await proxy.start()
        try:
            status, _reader, writer = await _connect(proxy.port, "127.0.0.1.nip.io:443")
            writer.close()
            return status, calls
        finally:
            await proxy.close()

    status, calls = _run(scenario())
    assert status.startswith("HTTP/1.1 403")
    assert calls == ["127.0.0.1.nip.io"]


def test_a_name_is_refused_when_any_of_its_addresses_is_forbidden():
    async def scenario():
        resolver = _fake_resolver({"mixed.azure.com": ["20.50.2.1", "169.254.169.254"]}, [])
        proxy = EgressProxy(_REAL_POLICY, resolver)
        await proxy.start()
        try:
            status, _reader, writer = await _connect(proxy.port, "mixed.azure.com:443")
            writer.close()
            return status
        finally:
            await proxy.close()

    assert _run(scenario()).startswith("HTTP/1.1 403")


def test_a_private_address_is_refused_for_a_host_that_is_not_an_internal_portal():
    async def scenario():
        proxy = EgressProxy(_REAL_POLICY, _fake_resolver({"intranet.example": ["10.0.0.5"]}, []))
        await proxy.start()
        try:
            status, _reader, writer = await _connect(proxy.port, "intranet.example:443")
            writer.close()
            return status
        finally:
            await proxy.close()

    assert _run(scenario()).startswith("HTTP/1.1 403")


def test_a_tunnel_opens_to_the_checked_address_and_carries_bytes_both_ways():
    """`rebind.test` exists in no real DNS: the tunnel can only work if the
    proxy connects to the address its resolver returned, never looking the
    name up again."""

    async def scenario():
        async def echo(reader, writer):
            writer.write(await reader.read(100))
            await writer.drain()
            writer.close()

        upstream = await asyncio.start_server(echo, "127.0.0.1", 0)
        upstream_port = upstream.sockets[0].getsockname()[1]
        calls = []
        proxy = EgressProxy(_LetEverythingThrough(), _fake_resolver({"rebind.test": ["127.0.0.1"]}, calls))
        await proxy.start()
        try:
            status, reader, writer = await _connect(proxy.port, f"rebind.test:{upstream_port}")
            await reader.readline()  # the blank line ending the proxy's response
            writer.write(b"hello")
            await writer.drain()
            echoed = await asyncio.wait_for(reader.read(100), 5)
            writer.close()
            return status, echoed, calls
        finally:
            await proxy.close()
            upstream.close()
            await upstream.wait_closed()

    status, echoed, calls = _run(scenario())
    assert status.startswith("HTTP/1.1 200")
    assert echoed == b"hello"
    assert calls == ["rebind.test"]


def test_a_plain_http_request_is_refused():
    async def scenario():
        calls = []
        proxy = EgressProxy(_LetEverythingThrough(), _fake_resolver({}, calls))
        await proxy.start()
        try:
            reader, writer = await asyncio.open_connection("127.0.0.1", proxy.port)
            writer.write(b"GET http://portal.azure.com/ HTTP/1.1\r\nHost: portal.azure.com\r\n\r\n")
            await writer.drain()
            status = (await reader.readline()).decode()
            writer.close()
            return status, calls
        finally:
            await proxy.close()

    status, calls = _run(scenario())
    assert status.startswith("HTTP/1.1 403")
    assert calls == []


def test_a_malformed_target_is_refused():
    async def scenario():
        proxy = EgressProxy(_LetEverythingThrough(), _fake_resolver({}, []))
        await proxy.start()
        try:
            status, _reader, writer = await _connect(proxy.port, "no-port-here")
            writer.close()
            return status
        finally:
            await proxy.close()

    assert _run(scenario()).startswith("HTTP/1.1 403")


def test_the_guard_check_host_is_recorded_and_never_resolved():
    async def scenario():
        calls = []
        proxy = EgressProxy(_REAL_POLICY, _fake_resolver({}, calls))
        await proxy.start()
        try:
            assert proxy.guard_check_seen is False
            status, _reader, writer = await _connect(proxy.port, f"{GUARD_CHECK_HOST}:443")
            writer.close()
            return status, calls, proxy.guard_check_seen
        finally:
            await proxy.close()

    status, calls, seen = _run(scenario())
    assert status.startswith("HTTP/1.1 403")
    assert calls == []
    assert seen is True


def test_a_new_policy_applies_to_the_next_connection():
    async def scenario():
        resolver = _fake_resolver({"jira.internal": ["10.0.0.5"]}, [])
        attempts = []

        async def refuse_at_once(address, port):
            attempts.append(address)
            raise ConnectionRefusedError()

        proxy = EgressProxy(
            RunnerUrlPolicy.from_json({"allowed_domains": ["*.azure.com"], "internal_hosts": [], "min_runner_version": None}),
            resolver,
            refuse_at_once,
        )
        await proxy.start()
        try:
            before, _r, w = await _connect(proxy.port, "jira.internal:443")
            w.close()
            proxy.set_policy(_REAL_POLICY)  # names jira.internal as an internal portal
            # Allowed now, so the proxy tries to connect: the stand-in
            # connector refuses, which shows up as 502, not the policy's 403.
            after, _r, w = await _connect(proxy.port, "jira.internal:443")
            w.close()
            return before, after, attempts
        finally:
            await proxy.close()

    before, after, attempts = _run(scenario())
    assert before.startswith("HTTP/1.1 403")
    assert after.startswith("HTTP/1.1 502")
    assert attempts == ["10.0.0.5"]
