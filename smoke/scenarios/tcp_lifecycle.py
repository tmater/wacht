from __future__ import annotations

import json
import time
import uuid

from client import SmokeError, wait_for
from scenarios.quorum import healthy_status, open_incident, resolved_incident


# Prove a TCP check can connect successfully, open one incident when the target
# stops accepting connections, and resolve after the listener comes back.
def run(server, mock):
    server.wait_for_health()
    mock.set_tcp_state("up")
    token = server.login()
    check_id = f"smoke-tcp-{uuid.uuid4().hex[:8]}"
    payload = {
        "id": check_id,
        "type": "tcp",
        "target": "mock:9091",
        "interval": 1,
    }
    server.create_check(token, payload)

    try:
        wait_for(
            "tcp check to become healthy before the outage",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: healthy_status(server, token, check_id),
        )

        assert_check_definition(server, token, check_id, expected_target="mock:9091")

        mock.set_tcp_state("down")

        opened = wait_for(
            "real probes to open a tcp incident after the listener goes down",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: open_incident(server, token, check_id),
        )

        assert_incident_stable(server, token, check_id, seconds=4)

        mock.set_tcp_state("up")

        resolved = wait_for(
            "real probes to resolve the tcp incident after the listener recovers",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: resolved_incident(server, token, check_id),
        )

        print(
            json.dumps(
                {
                    "check_id": check_id,
                    "opened": opened,
                    "resolved": resolved,
                },
                indent=2,
            )
        )
    finally:
        mock.set_tcp_state("up")
        server.delete_check_if_present(token, check_id)


def assert_check_definition(server, token, check_id, expected_target):
    checks = [check for check in server.list_checks(token) if check.get("id") == check_id]
    if len(checks) != 1:
        raise SmokeError(f"expected exactly 1 tcp check row for {check_id}, got {len(checks)}")
    check = checks[0]
    if check.get("type") != "tcp":
        raise SmokeError(f"expected tcp check type for {check_id}, got {check}")
    if check.get("target") != expected_target:
        raise SmokeError(f"expected tcp check target {expected_target!r} for {check_id}, got {check}")


def assert_incident_stable(server, token, check_id, seconds):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        opened = open_incident(server, token, check_id)
        if opened is None:
            raise SmokeError(f"expected exactly 1 open incident for tcp check {check_id} while down")
        time.sleep(1)
