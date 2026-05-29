"""Unit tests for app.auth.jwt.get_user_id_from_request."""
import asyncio
import time
from unittest.mock import AsyncMock, MagicMock, patch

import jwt as pyjwt
import pytest
from fastapi import HTTPException, Request

from tests.conftest import JWT_SECRET, TEST_USER_ID, make_token

# ── helpers ──────────────────────────────────────────────────────────────────

def _make_request(token: str | None = None, header: str | None = None) -> Request:
    """Build a minimal ASGI Request with the given Authorization header."""
    if header is not None:
        auth = header
    elif token is not None:
        auth = f"Bearer {token}"
    else:
        auth = None

    headers = []
    if auth is not None:
        headers.append((b"authorization", auth.encode()))

    scope = {
        "type": "http",
        "method": "GET",
        "path": "/",
        "headers": headers,
    }
    return Request(scope)


def _run(coro):
    """Run a coroutine synchronously — avoids pytest-asyncio for simple cases."""
    return asyncio.get_event_loop().run_until_complete(coro)


def _redis_not_revoked() -> MagicMock:
    """Return a mock async Redis client where exists() returns 0 (not revoked)."""
    m = MagicMock()
    m.exists = AsyncMock(return_value=0)
    return m


# ── tests ─────────────────────────────────────────────────────────────────────

def test_valid_token_returns_user_id():
    token = make_token()
    request = _make_request(token)

    with patch("app.auth.jwt.settings") as mock_settings, \
         patch("app.auth.jwt.get_redis", return_value=_redis_not_revoked()):
        mock_settings.jwt_secret = JWT_SECRET
        from app.auth.jwt import get_user_id_from_request
        result = _run(get_user_id_from_request(request))

    assert result == TEST_USER_ID


def test_missing_authorization_header_raises_401():
    request = _make_request()

    with patch("app.auth.jwt.settings") as mock_settings:
        mock_settings.jwt_secret = JWT_SECRET
        from app.auth.jwt import get_user_id_from_request
        with pytest.raises(HTTPException) as exc_info:
            _run(get_user_id_from_request(request))

    assert exc_info.value.status_code == 401


def test_missing_bearer_prefix_raises_401():
    request = _make_request(header="Token abc123")

    with patch("app.auth.jwt.settings") as mock_settings:
        mock_settings.jwt_secret = JWT_SECRET
        from app.auth.jwt import get_user_id_from_request
        with pytest.raises(HTTPException) as exc_info:
            _run(get_user_id_from_request(request))

    assert exc_info.value.status_code == 401


def test_expired_token_raises_401():
    token = make_token(exp_delta=-3600)  # expired 1 hour ago
    request = _make_request(token)

    with patch("app.auth.jwt.settings") as mock_settings:
        mock_settings.jwt_secret = JWT_SECRET
        from app.auth.jwt import get_user_id_from_request
        with pytest.raises(HTTPException) as exc_info:
            _run(get_user_id_from_request(request))

    assert exc_info.value.status_code == 401
    assert "expired" in exc_info.value.detail.lower()


def test_wrong_secret_raises_401():
    token = make_token(secret="different-secret")
    request = _make_request(token)

    with patch("app.auth.jwt.settings") as mock_settings:
        mock_settings.jwt_secret = JWT_SECRET
        from app.auth.jwt import get_user_id_from_request
        with pytest.raises(HTTPException) as exc_info:
            _run(get_user_id_from_request(request))

    assert exc_info.value.status_code == 401


def test_revoked_token_raises_401():
    """A token whose jti appears in the Redis denylist must be rejected."""
    token = make_token(jti="revoked-jti")
    request = _make_request(token)

    revoked_redis = MagicMock()
    revoked_redis.exists = AsyncMock(return_value=1)  # 1 = key exists = revoked

    with patch("app.auth.jwt.settings") as mock_settings, \
         patch("app.auth.jwt.get_redis", return_value=revoked_redis):
        mock_settings.jwt_secret = JWT_SECRET
        from app.auth.jwt import get_user_id_from_request
        with pytest.raises(HTTPException) as exc_info:
            _run(get_user_id_from_request(request))

    assert exc_info.value.status_code == 401
    assert "revoked" in exc_info.value.detail.lower()


def test_redis_unavailable_raises_503():
    """If Redis can't be reached, the function must fail closed (503)."""
    token = make_token(jti="some-jti")
    request = _make_request(token)

    broken_redis = MagicMock()
    broken_redis.exists = AsyncMock(side_effect=ConnectionError("redis down"))

    with patch("app.auth.jwt.settings") as mock_settings, \
         patch("app.auth.jwt.get_redis", return_value=broken_redis):
        mock_settings.jwt_secret = JWT_SECRET
        from app.auth.jwt import get_user_id_from_request
        with pytest.raises(HTTPException) as exc_info:
            _run(get_user_id_from_request(request))

    assert exc_info.value.status_code == 503


def test_token_without_jti_skips_denylist_check():
    """Tokens without a jti claim must still authenticate (no Redis lookup)."""
    token = make_token(jti=None)
    request = _make_request(token)

    # get_redis should NOT be called — if it is and raises, test fails.
    with patch("app.auth.jwt.settings") as mock_settings, \
         patch("app.auth.jwt.get_redis", side_effect=AssertionError("get_redis must not be called")):
        mock_settings.jwt_secret = JWT_SECRET
        from app.auth.jwt import get_user_id_from_request
        result = _run(get_user_id_from_request(request))

    assert result == TEST_USER_ID


def test_token_missing_sub_claim_raises_401():
    payload = {"exp": int(time.time()) + 3600, "jti": "j1"}
    token = pyjwt.encode(payload, JWT_SECRET, algorithm="HS256")
    request = _make_request(token)

    with patch("app.auth.jwt.settings") as mock_settings, \
         patch("app.auth.jwt.get_redis", return_value=_redis_not_revoked()):
        mock_settings.jwt_secret = JWT_SECRET
        from app.auth.jwt import get_user_id_from_request
        with pytest.raises(HTTPException) as exc_info:
            _run(get_user_id_from_request(request))

    assert exc_info.value.status_code == 401
