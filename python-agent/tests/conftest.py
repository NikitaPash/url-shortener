"""Shared test fixtures and configuration."""
import os
import time
from unittest.mock import patch

# app.main builds an OTLP BatchSpanProcessor at import time whose worker thread
# retries span export against Jaeger forever. Tests have no collector, so disable
# export BEFORE importing the app — otherwise the orphaned thread floods output
# with ConnectionRefused and crashes on pytest's closed capture stream
# ("I/O operation on closed file") at session end.
os.environ.setdefault("OTEL_ENABLED", "false")

import jwt as pyjwt  # noqa: E402
import pytest  # noqa: E402
from fastapi.testclient import TestClient  # noqa: E402

from app.main import app  # noqa: E402

JWT_SECRET = "test-secret-for-unit-tests"
TEST_USER_ID = "user-test-uuid-001"


def make_token(
    user_id: str = TEST_USER_ID,
    secret: str = JWT_SECRET,
    exp_delta: int = 3600,
    jti: str | None = "test-jti-001",
    extra: dict | None = None,
) -> str:
    """Create a signed HS256 JWT for test use."""
    payload: dict = {
        "sub": user_id,
        "iat": int(time.time()),
        "exp": int(time.time()) + exp_delta,
    }
    if jti is not None:
        payload["jti"] = jti
    if extra:
        payload.update(extra)
    return pyjwt.encode(payload, secret, algorithm="HS256")


@pytest.fixture(autouse=True)
def _suppress_prometheus(monkeypatch):
    """Prevent the Prometheus HTTP server from binding a port during tests.

    Patch the name as imported into app.main (``from prometheus_client import
    start_http_server``); patching ``prometheus_client.start_http_server`` here
    would miss the reference main already bound at import time.
    """
    with patch("app.main.start_http_server"):
        yield


@pytest.fixture
def client():
    """A FastAPI TestClient that triggers the full app lifespan."""
    with TestClient(app) as c:
        yield c
