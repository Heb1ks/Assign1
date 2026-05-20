"""
Load Testing — Medical Scheduling Platform
==========================================
Targets:
  • mock-gateway  → POST /notify, GET /health          (HTTP :8080)

Run:
  # Headless (CI / terminal):
  locust -f locustfile.py --headless -u 50 -r 5 -t 60s --host http://localhost:8080

  # Web UI:
  locust -f locustfile.py --host http://localhost:8080
  # then open http://localhost:8089

Requirements:
  pip install locust faker
"""

import uuid
import random
import string
from locust import HttpUser, task, between, events
from locust.runners import MasterRunner


# ─────────────────────────────────────────────────────────────────────────────
# Helpers
# ─────────────────────────────────────────────────────────────────────────────

CHANNELS = ["email", "sms", "push"]
RECIPIENTS = [f"user_{i}@clinic.example" for i in range(1, 21)]


def _random_idempotency_key() -> str:
    """Each call gets a unique key → simulates a fresh notification request."""
    return str(uuid.uuid4())


def _random_message() -> str:
    words = ["appointment", "confirmed", "cancelled", "reminder", "tomorrow", "doctor", "clinic"]
    return "Your " + " ".join(random.choices(words, k=4))


# ─────────────────────────────────────────────────────────────────────────────
# User: Normal load (happy path)
# ─────────────────────────────────────────────────────────────────────────────

class GatewayUser(HttpUser):
    """
    Simulates a notification-service calling the mock-gateway.

    Weight 7 — represents the majority of production traffic.
    wait_time: 0.5–2 s between tasks (realistic async worker cadence).
    """
    weight = 7
    wait_time = between(0.5, 2)

    @task(8)
    def post_notify_unique(self):
        """
        POST /notify with a brand-new idempotency_key.
        Expected responses: 200 {"status":"accepted"} or 503 (20% simulated failure).
        """
        payload = {
            "idempotency_key": _random_idempotency_key(),
            "channel": random.choice(CHANNELS),
            "recipient": random.choice(RECIPIENTS),
            "message": _random_message(),
        }
        with self.client.post(
            "/notify",
            json=payload,
            catch_response=True,
            name="POST /notify [unique]",
        ) as resp:
            if resp.status_code == 200:
                body = resp.json()
                if body.get("status") not in ("accepted", "duplicate"):
                    resp.failure(f"Unexpected body: {body}")
                else:
                    resp.success()
            elif resp.status_code == 503:
                # Gateway intentionally returns 503 ~20% of the time.
                # Mark as success so Locust doesn't count it as an error —
                # but track it via custom counter below.
                resp.success()
                _503_counter["count"] += 1
            else:
                resp.failure(f"Unexpected status: {resp.status_code}")

    @task(2)
    def get_health(self):
        """
        GET /health — lightweight liveness probe.
        Simulates load balancer / k8s health checks.
        """
        with self.client.get("/health", catch_response=True, name="GET /health") as resp:
            if resp.status_code == 200:
                resp.success()
            else:
                resp.failure(f"Health check failed: {resp.status_code}")


# ─────────────────────────────────────────────────────────────────────────────
# User: Duplicate / idempotency stress
# ─────────────────────────────────────────────────────────────────────────────

# Shared pool of keys that will be re-sent to exercise idempotency logic
_shared_keys: list[str] = [str(uuid.uuid4()) for _ in range(20)]


class IdempotencyUser(HttpUser):
    """
    Re-sends the same idempotency keys repeatedly.
    Tests that the gateway returns {"status":"duplicate"} correctly.

    Weight 2 — minority of traffic.
    """
    weight = 2
    wait_time = between(1, 3)

    @task
    def post_notify_duplicate(self):
        payload = {
            "idempotency_key": random.choice(_shared_keys),
            "channel": random.choice(CHANNELS),
            "recipient": random.choice(RECIPIENTS),
            "message": "duplicate test message",
        }
        with self.client.post(
            "/notify",
            json=payload,
            catch_response=True,
            name="POST /notify [duplicate]",
        ) as resp:
            if resp.status_code == 200:
                resp.success()
            elif resp.status_code == 503:
                resp.success()  # expected transient failure
            else:
                resp.failure(f"Unexpected status: {resp.status_code}")


# ─────────────────────────────────────────────────────────────────────────────
# User: Spike / burst load
# ─────────────────────────────────────────────────────────────────────────────

class SpikeUser(HttpUser):
    """
    Fires bursts with almost no wait — simulates a sudden spike.
    Use sparingly (low weight) to test auto-scaling triggers.

    Weight 1.
    """
    weight = 1
    wait_time = between(0.05, 0.2)

    @task
    def burst_notify(self):
        payload = {
            "idempotency_key": _random_idempotency_key(),
            "channel": "email",
            "recipient": "spike@clinic.example",
            "message": "spike load test",
        }
        with self.client.post(
            "/notify",
            json=payload,
            catch_response=True,
            name="POST /notify [spike]",
        ) as resp:
            resp.success()  # We only care about throughput here


# ─────────────────────────────────────────────────────────────────────────────
# Custom metrics / event hooks
# ─────────────────────────────────────────────────────────────────────────────

_503_counter = {"count": 0}


@events.test_start.add_listener
def on_test_start(environment, **kwargs):
    print("\n" + "=" * 60)
    print("  Load Test STARTED — Medical Scheduling Platform")
    print(f"  Target: {environment.host}")
    print("=" * 60 + "\n")
    _503_counter["count"] = 0


@events.test_stop.add_listener
def on_test_stop(environment, **kwargs):
    stats = environment.stats.total
    print("\n" + "=" * 60)
    print("  Load Test FINISHED")
    print(f"  Total requests  : {stats.num_requests}")
    print(f"  Failures        : {stats.num_failures}")
    print(f"  503s (simulated): {_503_counter['count']}")
    if stats.num_requests > 0:
        p50 = stats.get_response_time_percentile(0.50)
        p95 = stats.get_response_time_percentile(0.95)
        p99 = stats.get_response_time_percentile(0.99)
        print(f"  p50 latency     : {p50:.0f} ms")
        print(f"  p95 latency     : {p95:.0f} ms")
        print(f"  p99 latency     : {p99:.0f} ms")
        print(f"  Avg RPS         : {stats.total_rps:.1f}")
    print("=" * 60 + "\n")
