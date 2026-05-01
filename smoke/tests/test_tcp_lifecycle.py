from __future__ import annotations

import json
import time
import uuid

from smoke.client import SmokeError, wait_for
from smoke.support.cleanup import CleanupScope
from smoke.support.quorum import healthy_status, open_incident, resolved_incident


# Prove a TCP check can connect successfully, open one incident when the target
# stops accepting connections, and resolve after the listener comes back.
def test_tcp_lifecycle(server, mock):
    server.wait_for_health()
    mock.set_tcp_state("up")
    token = server.login()
    check_name = f"smoke-tcp-{uuid.uuid4().hex[:8]}"
    payload = {
        "name": check_name,
        "type": "tcp",
        "target": "mock:9091",
        "interval": 1,
    }
    cleanup = CleanupScope()
    server.create_check(token, payload)

    try:
        with cleanup.preserve_primary_error():
            wait_for(
                "tcp check to become healthy before the outage",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: healthy_status(server, token, check_name),
            )

            assert_check_definition(server, token, check_name, expected_target="mock:9091")

            mock.set_tcp_state("down")

            opened = wait_for(
                "real probes to open a tcp incident after the listener goes down",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: open_incident(server, token, check_name),
            )

            assert_incident_stable(server, token, check_name, seconds=4)

            mock.set_tcp_state("up")

            resolved = wait_for(
                "real probes to resolve the tcp incident after the listener recovers",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: resolved_incident(server, token, check_name),
            )

            print(
                json.dumps(
                    {
                        "check_name": check_name,
                        "opened": opened,
                        "resolved": resolved,
                    },
                    indent=2,
                )
            )
    finally:
        cleanup.run("restore mock TCP state", lambda: mock.set_tcp_state("up"))
        cleanup.run(f"delete check {check_name}", lambda: server.delete_check_if_present(token, check_name))
        cleanup.finish()


def assert_check_definition(server, token, check_name, expected_target):
    checks = [check for check in server.list_checks(token) if check.get("name") == check_name]
    if len(checks) != 1:
        raise SmokeError(f"expected exactly 1 tcp check row for {check_name}, got {len(checks)}")
    check = checks[0]
    if check.get("type") != "tcp":
        raise SmokeError(f"expected tcp check type for {check_name}, got {check}")
    if check.get("target") != expected_target:
        raise SmokeError(f"expected tcp check target {expected_target!r} for {check_name}, got {check}")


def assert_incident_stable(server, token, check_name, seconds):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        opened = open_incident(server, token, check_name)
        if opened is None:
            raise SmokeError(f"expected exactly 1 open incident for tcp check {check_name} while down")
        time.sleep(1)
