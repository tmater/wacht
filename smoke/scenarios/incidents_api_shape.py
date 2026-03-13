from __future__ import annotations

import json
import uuid
from datetime import datetime

from client import SmokeError, wait_for
from scenarios.quorum import healthy_status, open_incident, resolved_incident, status_for_check


HTTP_TARGET = "http://mock:9090/http/state"
TCP_TARGET = "mock:9091"


# Prove /api/incidents keeps incidents linked to the right check, returns
# resolved fields only after recovery, and stays ordered newest-first.
def run(server, mock):
    server.wait_for_health()
    mock.set_state("up")
    mock.set_tcp_state("up")
    token = server.login()
    suffix = uuid.uuid4().hex[:8]
    http_check_id = f"smoke-incidents-http-{suffix}"
    tcp_check_id = f"smoke-incidents-tcp-{suffix}"

    server.create_check(
        token,
        {
            "id": http_check_id,
            "type": "http",
            "target": HTTP_TARGET,
            "interval": 1,
        },
    )
    server.create_check(
        token,
        {
            "id": tcp_check_id,
            "type": "tcp",
            "target": TCP_TARGET,
            "interval": 1,
        },
    )

    try:
        wait_for(
            "http incident-shape check to become healthy",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: healthy_status(server, token, http_check_id),
        )
        wait_for(
            "tcp incident-shape check to become healthy",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: healthy_status(server, token, tcp_check_id),
        )

        if incidents_for_checks(server, token, http_check_id, tcp_check_id):
            raise SmokeError("expected no incident rows before either target goes down")

        mock.set_state("down")

        http_opened = wait_for(
            "http outage to open the first incident row",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: open_incident(server, token, http_check_id),
        )
        http_open = http_opened["incidents"][0]
        assert_open_incident(http_open, http_check_id)
        assert_open_status(http_opened["status"], http_open, http_check_id)
        assert_check_up(server, token, tcp_check_id)

        history = incidents_for_checks(server, token, http_check_id, tcp_check_id)
        assert_history(history, [(http_check_id, False)])

        mock.set_state("up")

        http_resolved = wait_for(
            "http recovery to resolve the first incident row",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: resolved_incident(server, token, http_check_id),
        )
        resolved_http_incident = http_resolved["incidents"][0]
        assert_resolved_incident(resolved_http_incident, http_check_id)
        assert_cleared_status(http_resolved["status"], http_check_id)
        assert_check_up(server, token, tcp_check_id)

        mock.set_tcp_state("down")

        tcp_opened = wait_for(
            "tcp outage to open the second incident row",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: open_incident(server, token, tcp_check_id),
        )
        tcp_open = tcp_opened["incidents"][0]
        assert_open_incident(tcp_open, tcp_check_id)
        assert_open_status(tcp_opened["status"], tcp_open, tcp_check_id)
        assert_check_up(server, token, http_check_id)

        history = incidents_for_checks(server, token, http_check_id, tcp_check_id)
        assert_history(history, [(tcp_check_id, False), (http_check_id, True)])

        if history[0].get("id") == history[1].get("id"):
            raise SmokeError(f"expected distinct incident ids for {tcp_check_id} and {http_check_id}, got {history}")

        mock.set_tcp_state("up")

        tcp_resolved = wait_for(
            "tcp recovery to resolve the second incident row",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: resolved_incident(server, token, tcp_check_id),
        )
        resolved_tcp_incident = tcp_resolved["incidents"][0]
        assert_resolved_incident(resolved_tcp_incident, tcp_check_id)
        assert_cleared_status(tcp_resolved["status"], tcp_check_id)
        assert_check_up(server, token, http_check_id)

        history = incidents_for_checks(server, token, http_check_id, tcp_check_id)
        assert_history(history, [(tcp_check_id, True), (http_check_id, True)])

        print(
            json.dumps(
                {
                    "history": history,
                    "http_incident": resolved_http_incident,
                    "tcp_incident": resolved_tcp_incident,
                },
                indent=2,
            )
        )
    finally:
        mock.set_state("up")
        mock.set_tcp_state("up")
        server.delete_check_if_present(token, http_check_id)
        server.delete_check_if_present(token, tcp_check_id)


def incidents_for_checks(server, token, *check_ids):
    wanted = set(check_ids)
    return [incident for incident in server.list_incidents(token) if incident.get("check_id") in wanted]


def assert_history(history, expected):
    if len(history) != len(expected):
        raise SmokeError(f"expected {len(expected)} incident rows, got {len(history)}: {history}")

    started_ats = []
    for incident, (check_id, resolved) in zip(history, expected):
        if incident.get("check_id") != check_id:
            raise SmokeError(f"expected incident for {check_id}, got {incident}")
        if resolved:
            assert_resolved_incident(incident, check_id)
        else:
            assert_open_incident(incident, check_id)
        started_ats.append(parse_timestamp(incident.get("started_at"), "started_at", check_id))

    for newer, older in zip(started_ats, started_ats[1:]):
        if newer <= older:
            raise SmokeError(f"expected incidents newest-first by started_at, got {history}")


def assert_open_status(status, incident, check_id):
    if status.get("status") != "down":
        raise SmokeError(f"expected {check_id} status=down during the outage, got {status}")
    if status.get("incident_since") != incident.get("started_at"):
        raise SmokeError(
            f"expected {check_id} incident_since={incident.get('started_at')!r}, got {status.get('incident_since')!r}"
        )


def assert_cleared_status(status, check_id):
    if status.get("status") != "up":
        raise SmokeError(f"expected {check_id} status=up after recovery, got {status}")
    if "incident_since" in status:
        raise SmokeError(f"expected {check_id} incident_since to be omitted after recovery, got {status}")


def assert_check_up(server, token, check_id):
    status = status_for_check(server, token, check_id)
    if status is None:
        raise SmokeError(f"expected {check_id} to remain visible in /status")
    if status.get("status") != "up":
        raise SmokeError(f"expected {check_id} to remain up, got {status}")
    if "incident_since" in status:
        raise SmokeError(f"expected {check_id} to have no incident_since while healthy, got {status}")


def assert_open_incident(incident, check_id):
    assert_common_incident_fields(incident, check_id)
    if "resolved_at" in incident:
        raise SmokeError(f"expected open incident for {check_id} to omit resolved_at, got {incident}")
    if "duration_ms" in incident:
        raise SmokeError(f"expected open incident for {check_id} to omit duration_ms, got {incident}")


def assert_resolved_incident(incident, check_id):
    started_at = assert_common_incident_fields(incident, check_id)

    if "resolved_at" not in incident:
        raise SmokeError(f"expected resolved incident for {check_id} to include resolved_at, got {incident}")
    resolved_at = parse_timestamp(incident.get("resolved_at"), "resolved_at", check_id)
    if resolved_at <= started_at:
        raise SmokeError(f"expected resolved_at after started_at for {check_id}, got {incident}")

    if "duration_ms" not in incident:
        raise SmokeError(f"expected resolved incident for {check_id} to include duration_ms, got {incident}")
    duration_ms = incident.get("duration_ms")
    if not isinstance(duration_ms, int):
        raise SmokeError(f"expected duration_ms int for {check_id}, got {incident}")
    if duration_ms <= 0:
        raise SmokeError(f"expected duration_ms > 0 for {check_id}, got {incident}")


def assert_common_incident_fields(incident, check_id):
    incident_id = incident.get("id")
    if not isinstance(incident_id, int) or incident_id <= 0:
        raise SmokeError(f"expected positive integer incident id for {check_id}, got {incident}")
    if incident.get("check_id") != check_id:
        raise SmokeError(f"expected incident check_id={check_id!r}, got {incident}")
    return parse_timestamp(incident.get("started_at"), "started_at", check_id)


def parse_timestamp(value, field_name, check_id):
    if not isinstance(value, str) or not value:
        raise SmokeError(f"expected {field_name} string for {check_id}, got {value!r}")
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as exc:
        raise SmokeError(f"expected {field_name} RFC3339 timestamp for {check_id}, got {value!r}") from exc
