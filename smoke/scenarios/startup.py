from __future__ import annotations

import json

from client import SmokeError, wait_for


# Prove the packaged stack can boot, accept auth, and show a real probe-driven
# result in the authenticated status view.
def run(client):
    client.wait_for_health()
    token = client.login()

    def ready():
        status = client.get_status(token)
        checks = {check["check_id"]: check for check in status.get("checks", [])}
        probes = {probe["probe_id"]: probe for probe in status.get("probes", [])}

        check = checks.get("check-self")
        probe = probes.get("probe-smoke")
        if check is None:
            return None
        if probe is None:
            return None
        if check.get("status") != "up":
            return None
        if not probe.get("online", False):
            return None
        return status

    status = wait_for(
        "seeded check to become up and seeded probe to become online",
        timeout_seconds=90,
        interval_seconds=3,
        fn=ready,
    )

    # Print the final state so CI logs show what the smoke scenario actually
    # observed when it succeeded.
    print(json.dumps(status, indent=2))

    checks = {check["check_id"]: check for check in status.get("checks", [])}
    if "check-self" not in checks:
        raise SmokeError("seeded check-self is missing from /status")
