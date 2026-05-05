from __future__ import annotations

import json
import uuid

from smoke.client import SmokeError, wait_for


def test_admin_can_provision_probe_credential(server, stack):
    server.wait_for_health()

    admin_token = server.login()
    probe_id = f"probe-smoke-{uuid.uuid4().hex[:12]}"
    reset_stack = False

    try:
        credential = server.create_probe_credential(admin_token, probe_id)
        reset_stack = True

        if credential.get("probe_id") != probe_id:
            raise SmokeError(f"expected probe_id {probe_id!r}, got {credential!r}")
        secret = credential.get("secret")
        if not secret:
            raise SmokeError(f"expected generated probe secret, got {credential!r}")

        headers = probe_headers(probe_id, secret)
        register = server.request(
            "POST",
            "/api/probes/register",
            payload={"probe_id": probe_id, "version": "smoke-test"},
            headers=headers,
            expected_status=(204,),
        )
        heartbeat = server.request(
            "POST",
            "/api/probes/heartbeat",
            payload={"probe_id": probe_id},
            headers=headers,
            expected_status=(204,),
        )

        probe = wait_for(
            f"{probe_id} to authenticate and become visible",
            timeout_seconds=30,
            interval_seconds=2,
            fn=lambda: status_probe(server, admin_token, probe_id),
        )

        print(
            json.dumps(
                {
                    "created_probe_id": credential.get("probe_id"),
                    "secret_length": len(secret),
                    "register": register,
                    "heartbeat": heartbeat,
                    "status_probe": probe,
                },
                indent=2,
            )
        )
    finally:
        if reset_stack:
            stack.down()
            stack.up()
            server.wait_for_health()


def status_probe(server, token, probe_id):
    status = server.get_status(token)
    probes = {probe["probe_id"]: probe for probe in status.get("probes", [])}
    probe = probes.get(probe_id)
    if probe is None or not probe.get("online", False):
        return None
    return probe


def probe_headers(probe_id, secret):
    return {
        "X-Wacht-Probe-ID": probe_id,
        "X-Wacht-Probe-Secret": secret,
    }
