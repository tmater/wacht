from __future__ import annotations

import json
import time
import uuid

from smoke.client import SmokeError, wait_for
from smoke.support.cleanup import CleanupScope
from smoke.support.quorum import healthy_status, open_incident, resolved_incident


DNS_TARGET = "smoke-dns.wacht.test"


# Prove a DNS check resolves successfully, opens one incident when the fixture
# hostname stops resolving, and resolves again after DNS recovery.
def test_dns_lifecycle(server, mock):
    server.wait_for_health()
    mock.request("POST", "/dns/state", payload={"status": "up"}, expected_status=(204,))
    token = server.login()
    check_name = f"smoke-dns-{uuid.uuid4().hex[:8]}"
    payload = {
        "name": check_name,
        "type": "dns",
        "target": DNS_TARGET,
        "interval": 1,
    }
    cleanup = CleanupScope()
    server.create_check(token, payload)

    try:
        with cleanup.preserve_primary_error():
            wait_for(
                "dns check to become healthy before the outage",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: healthy_status(server, token, check_name),
            )

            assert_check_definition(server, token, check_name, expected_target=DNS_TARGET)

            mock.request("POST", "/dns/state", payload={"status": "down"}, expected_status=(204,))

            opened = wait_for(
                "real probes to open a dns incident after the hostname stops resolving",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: open_incident(server, token, check_name),
            )

            assert_incident_stable(server, token, check_name, seconds=4)

            mock.request("POST", "/dns/state", payload={"status": "up"}, expected_status=(204,))

            resolved = wait_for(
                "real probes to resolve the dns incident after the hostname resolves again",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: resolved_incident(server, token, check_name),
            )

            print(
                json.dumps(
                    {
                        "check_name": check_name,
                        "target": DNS_TARGET,
                        "opened": opened,
                        "resolved": resolved,
                    },
                    indent=2,
                )
            )
    finally:
        cleanup.run(
            "restore mock DNS state",
            lambda: mock.request("POST", "/dns/state", payload={"status": "up"}, expected_status=(204,)),
        )
        cleanup.run(f"delete check {check_name}", lambda: server.delete_check_if_present(token, check_name))
        cleanup.finish()


def assert_check_definition(server, token, check_name, expected_target):
    checks = [check for check in server.list_checks(token) if check.get("name") == check_name]
    if len(checks) != 1:
        raise SmokeError(f"expected exactly 1 dns check row for {check_name}, got {len(checks)}")
    check = checks[0]
    if check.get("type") != "dns":
        raise SmokeError(f"expected dns check type for {check_name}, got {check}")
    if check.get("target") != expected_target:
        raise SmokeError(f"expected dns check target {expected_target!r} for {check_name}, got {check}")


def assert_incident_stable(server, token, check_name, seconds):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        opened = open_incident(server, token, check_name)
        if opened is None:
            raise SmokeError(f"expected exactly 1 open incident for dns check {check_name} while down")
        time.sleep(1)
