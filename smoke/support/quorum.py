from __future__ import annotations


def healthy_status(server, token, check_id):
    status = status_for_check(server, token, check_id)
    if status is None:
        return None
    if status.get("status") != "up":
        return None
    if status.get("incident_since") is not None:
        return None
    return status


def open_incident(server, token, check_id):
    status = status_for_check(server, token, check_id)
    if status is None:
        return None
    if status.get("status") != "down":
        return None
    if status.get("incident_since") is None:
        return None

    incidents = incidents_for_check(server, token, check_id)
    if len(incidents) != 1:
        return None
    if incidents[0].get("resolved_at") is not None:
        return None
    return {"status": status, "incidents": incidents}


def resolved_incident(server, token, check_id):
    status = status_for_check(server, token, check_id)
    if status is None:
        return None
    if status.get("status") != "up":
        return None
    if status.get("incident_since") is not None:
        return None

    incidents = incidents_for_check(server, token, check_id)
    if len(incidents) != 1:
        return None
    if incidents[0].get("resolved_at") is None:
        return None
    return {"status": status, "incidents": incidents}


def status_for_check(server, token, check_id):
    checks = server.get_status(token).get("checks", [])
    return next((check for check in checks if check.get("check_id") == check_id), None)


def incidents_for_check(server, token, check_id):
    return [incident for incident in server.list_incidents(token) if incident.get("check_id") == check_id]
