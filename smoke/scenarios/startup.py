from __future__ import annotations

import json
import uuid

from client import SmokeError, wait_for


# Prove the packaged stack can boot, accept auth, and show a real probe-driven
# result in the authenticated status view.
def run(server, mock):
    server.wait_for_health()
    mock.set_state("up")
    token = server.login()
    check_id = f"smoke-startup-{uuid.uuid4().hex[:8]}"
    payload = {
        "id": check_id,
        "type": "http",
        "target": "http://mock:9090/http/state",
        "interval": 1,
    }
    server.create_check(token, payload)

    try:
        def ready():
            status = server.get_status(token)
            checks = {check["check_id"]: check for check in status.get("checks", [])}
            probes = {probe["probe_id"]: probe for probe in status.get("probes", [])}

            check = checks.get(check_id)
            if check is None:
                return None
            if check.get("status") != "up":
                return None

            expected_probes = ("probe-1", "probe-2", "probe-3")
            for probe_id in expected_probes:
                probe = probes.get(probe_id)
                if probe is None:
                    return None
                if not probe.get("online", False):
                    return None
            return status

        status = wait_for(
            "created check to become up and all three probes to become online",
            timeout_seconds=90,
            interval_seconds=3,
            fn=ready,
        )

        # Print the final state so CI logs show what the smoke scenario actually
        # observed when it succeeded.
        print(json.dumps(status, indent=2))

        checks = {check["check_id"]: check for check in status.get("checks", [])}
        if check_id not in checks:
            raise SmokeError(f"created startup check {check_id} is missing from /status")
    finally:
        server.delete_check_if_present(token, check_id)
