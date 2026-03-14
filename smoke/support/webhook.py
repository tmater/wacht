from __future__ import annotations

import time

from smoke.client import SmokeError


def expected_deliveries(mock, check_id, expected, statuses):
    deliveries = deliveries_for_check(mock, check_id)
    if len(deliveries) != expected:
        return None
    if delivery_statuses(deliveries) != statuses:
        return None
    assert_delivery_payloads(deliveries, check_id)
    return deliveries


def assert_deliveries_stable(mock, check_id, expected, statuses, seconds):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        deliveries = deliveries_for_check(mock, check_id)
        if len(deliveries) != expected:
            raise SmokeError(f"expected {expected} webhook deliveries for {check_id}, got {len(deliveries)}")
        if delivery_statuses(deliveries) != statuses:
            raise SmokeError(f"expected webhook statuses {statuses} for {check_id}, got {delivery_statuses(deliveries)}")
        assert_delivery_payloads(deliveries, check_id)
        time.sleep(1)


def deliveries_for_check(mock, check_id):
    return [payload for payload in mock.list_webhooks() if payload.get("check_id") == check_id]


def delivery_statuses(deliveries):
    return [payload.get("status") for payload in deliveries]


def assert_delivery_payloads(deliveries, check_id):
    for payload in deliveries:
        if payload.get("check_id") != check_id:
            raise SmokeError(f"expected webhook check_id {check_id}, got {payload}")
        if payload.get("target") != "http://mock:9090/http/state":
            raise SmokeError(f"expected webhook target to be the smoke mock state endpoint, got {payload}")
        if payload.get("probes_total") != 3:
            raise SmokeError(f"expected webhook probes_total=3 for {check_id}, got {payload}")
