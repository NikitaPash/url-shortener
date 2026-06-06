"""§6.8 — Python agent ↔ Redis JWT logout denylist (L1).

Proves the cross-service revocation contract end-to-end against REAL Redis: a
token whose ``jti`` the Go API wrote to ``jwt:deny:`` on logout is rejected (401),
and if the denylist backend can't be reached the check fails CLOSED (503) rather
than letting a possibly-revoked token through. The matching Go side is covered by
its own integration suite (§6.2); here we prove the Python reader honours the
shared ``jwt:deny:<jti>`` keyspace.
"""
import uuid

import pytest
from fastapi import HTTPException, Request

import app.db.redis_client as redis_module
from app.auth.jwt import JWT_DENY_PREFIX, get_user_id_from_request
from app.config import settings
from tests.conftest import TEST_USER_ID, make_token

pytestmark = pytest.mark.integration


def _request_with_token(token: str) -> Request:
    """Minimal ASGI request carrying a Bearer token, as FastAPI would build."""
    scope = {
        "type": "http",
        "method": "POST",
        "path": "/api/query",
        "headers": [(b"authorization", f"Bearer {token}".encode())],
    }
    return Request(scope)


async def test_revoked_jti_is_rejected(agent_redis, redis_client):
    """6.8.1 — a jti present in the Redis denylist → 401, even though the signature
    is valid and unexpired."""
    jti = "it-revoked-" + uuid.uuid4().hex
    token = make_token(jti=jti)
    redis_client.set(f"{JWT_DENY_PREFIX}{jti}", "1")
    try:
        with pytest.raises(HTTPException) as exc:
            await get_user_id_from_request(_request_with_token(token))
        assert exc.value.status_code == 401
        assert "revoked" in exc.value.detail.lower()
    finally:
        redis_client.delete(f"{JWT_DENY_PREFIX}{jti}")


async def test_non_revoked_token_authenticates(agent_redis, redis_client):
    """Positive control: a valid token whose jti is NOT denylisted passes against
    real Redis and yields the caller's user id."""
    jti = "it-live-" + uuid.uuid4().hex
    redis_client.delete(f"{JWT_DENY_PREFIX}{jti}")  # ensure absent
    token = make_token(jti=jti)

    user_id = await get_user_id_from_request(_request_with_token(token))
    assert user_id == TEST_USER_ID


async def test_redis_unreachable_fails_closed(agent_redis):
    """6.8.2 — if the denylist backend can't be reached, deny access (503) rather
    than fall open. Point the client at a closed port to simulate Redis down; the
    valid token still must not get through."""
    settings.redis_addr = "127.0.0.1:6390"  # nothing listening here
    redis_module._client = None
    token = make_token(jti="it-faildown-" + uuid.uuid4().hex)

    with pytest.raises(HTTPException) as exc:
        await get_user_id_from_request(_request_with_token(token))
    assert exc.value.status_code == 503
