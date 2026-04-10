from __future__ import annotations

import json
import uuid

from smoke.client import wait_for
from smoke.support.cleanup import CleanupScope
from smoke.support.quorum import healthy_status, open_incident


# Prove an already-open incident survives a server restart even when probes are
# stopped before the restart, so the recovered runtime cannot rely on fresh
# post-boot probe results to reconstruct the down state.
def test_restart_recovery_preserves_open_incident(server, mock, probes, stack):
    server.wait_for_health()
    mock.set_state("up")
    token = server.login()
    check_id = f"smoke-restart-recovery-{uuid.uuid4().hex[:8]}"
    cleanup = CleanupScope()
    stopped_probe_ids = []

    server.create_check(
        token,
        {
            "id": check_id,
            "type": "http",
            "target": "http://mock:9090/http/state",
            "interval": 1,
        },
    )

    try:
        with cleanup.preserve_primary_error():
            wait_for(
                "restart-recovery check to become healthy before outage",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: healthy_status(server, token, check_id),
            )

            mock.set_state("down")

            opened = wait_for(
                "restart-recovery outage to open one incident",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: open_incident(server, token, check_id),
            )

            for probe_id in ("probe-1", "probe-2", "probe-3"):
                probes.stop(probe_id)
                stopped_probe_ids.append(probe_id)

            stack.stop_service("server")
            stack.start_service("server")
            server.wait_for_health()
            token = server.login()

            recovered = wait_for(
                "restarted server to recover the open incident without fresh probe writes",
                timeout_seconds=20,
                interval_seconds=2,
                fn=lambda: open_incident(server, token, check_id),
            )

            print(json.dumps({"opened": opened, "recovered": recovered}, indent=2))
    finally:
        cleanup.run("restore mock HTTP state", lambda: mock.set_state("up"))
        for probe_id in stopped_probe_ids:
            cleanup.run(f"restart {probe_id}", lambda probe_id=probe_id: probes.restore(probe_id))
        cleanup.run(f"delete check {check_id}", lambda: server.delete_check_if_present(token, check_id))
        cleanup.finish()
