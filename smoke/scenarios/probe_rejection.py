from __future__ import annotations

import json
import uuid

from client import SmokeError


VALID_PROBE_ID = "probe-1"
VALID_PROBE_SECRET = "smoke-secret-1"


# Prove the probe-facing API rejects invalid credentials and inconsistent
# payloads before they can mutate probe state or check history.
def run(server, mock):
    del mock  # This scenario talks directly to the server's probe endpoints.
    server.wait_for_health()

    bad_secret = server.request(
        "POST",
        "/api/probes/heartbeat",
        payload={"probe_id": VALID_PROBE_ID},
        headers=probe_headers(secret="definitely-wrong"),
        expected_status=(401,),
    )
    assert_body("bad secret", bad_secret, "unauthorized\n")

    mismatched_probe = server.request(
        "POST",
        "/api/probes/register",
        payload={"probe_id": "probe-2", "version": "smoke-test"},
        headers=probe_headers(),
        expected_status=(400,),
    )
    assert_body(
        "mismatched probe_id",
        mismatched_probe,
        "probe_id does not match authenticated probe\n",
    )

    unknown_check_id = f"smoke-missing-{uuid.uuid4().hex[:8]}"
    unknown_check = server.request(
        "POST",
        "/api/results",
        payload={"check_id": unknown_check_id, "probe_id": VALID_PROBE_ID, "up": True},
        headers=probe_headers(),
        expected_status=(400,),
    )
    assert_body("unknown check_id", unknown_check, "unknown check_id\n")

    print(
        json.dumps(
            {
                "bad_secret": bad_secret.strip(),
                "mismatched_probe_id": mismatched_probe.strip(),
                "unknown_check_id": {
                    "check_id": unknown_check_id,
                    "response": unknown_check.strip(),
                },
            },
            indent=2,
        )
    )


def probe_headers(probe_id=VALID_PROBE_ID, secret=VALID_PROBE_SECRET):
    return {
        "X-Wacht-Probe-ID": probe_id,
        "X-Wacht-Probe-Secret": secret,
    }


def assert_body(label, body, expected):
    if body != expected:
        raise SmokeError(f"expected {label} response {expected!r}, got {body!r}")
