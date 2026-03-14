from __future__ import annotations

import json
import time
import uuid

from smoke.client import SmokeError, wait_for
from smoke.support.cleanup import CleanupScope
from smoke.support.quorum import healthy_status, incidents_for_check, open_incident, resolved_incident


# Prove a real outage observed by the real probes opens exactly one incident,
# and that recovery resolves that same incident again.
def test_quorum(server, mock):
    server.wait_for_health()
    mock.set_state("up")
    token = server.login()
    check_id = f"smoke-quorum-{uuid.uuid4().hex[:8]}"
    payload = {
        "id": check_id,
        "type": "http",
        "target": "http://mock:9090/http/state",
        "interval": 1,
    }
    cleanup = CleanupScope()
    server.create_check(token, payload)

    try:
        with cleanup.preserve_primary_error():
            wait_for(
                "quorum check to become healthy before the outage",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: healthy_status(server, token, check_id),
            )

            mock.set_state("down")

            opened = wait_for(
                "real probes to open an incident after the target goes down",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: open_incident(server, token, check_id),
            )

            # Keep the target down briefly to catch duplicate incident rows while
            # the outage is still ongoing.
            deadline = time.monotonic() + 4
            while time.monotonic() < deadline:
                incidents = incidents_for_check(server, token, check_id)
                if len(incidents) != 1:
                    raise SmokeError(f"expected exactly 1 open incident during outage, got {len(incidents)}")
                if incidents[0].get("resolved_at") is not None:
                    raise SmokeError("incident resolved while the target was still down")
                time.sleep(1)

            mock.set_state("up")

            resolved = wait_for(
                "real probes to resolve the incident after recovery",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: resolved_incident(server, token, check_id),
            )

            print(json.dumps({"opened": opened, "resolved": resolved}, indent=2))
    finally:
        cleanup.run("restore mock HTTP state", lambda: mock.set_state("up"))
        cleanup.run(f"delete check {check_id}", lambda: server.delete_check_if_present(token, check_id))
        cleanup.finish()
