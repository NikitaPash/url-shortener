import logging

import jwt
from fastapi import HTTPException, Request

from app.config import settings
from app.db.redis_client import get_redis

logger = logging.getLogger(__name__)

# Must match the key prefix the Go API uses when revoking tokens on logout.
JWT_DENY_PREFIX = "jwt:deny:"


async def get_user_id_from_request(request: Request) -> str:
    """Extract and verify the JWT from the Authorization header.

    Returns the user_id (sub claim) if the token is valid and not revoked.
    Raises HTTPException 401 if missing/invalid/revoked, 503 if the revocation
    backend can't be reached (fail closed).
    """
    auth_header = request.headers.get("Authorization", "")
    if not auth_header.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="Missing or invalid Authorization header")

    token = auth_header[7:]

    try:
        payload = jwt.decode(
            token,
            settings.jwt_secret,
            algorithms=["HS256"],
        )
    except jwt.ExpiredSignatureError:
        raise HTTPException(status_code=401, detail="Token expired")
    except jwt.InvalidTokenError:
        raise HTTPException(status_code=401, detail="Invalid token")

    user_id = payload.get("sub")
    if not user_id:
        raise HTTPException(status_code=401, detail="Token missing user ID")

    # Honor the same logout/revocation denylist the Go API writes to Redis, so a
    # token that was logged out can't keep querying analytics until it expires.
    jti = payload.get("jti")
    if jti:
        await _reject_if_revoked(jti)

    return user_id


async def _reject_if_revoked(jti: str) -> None:
    try:
        revoked = await get_redis().exists(f"{JWT_DENY_PREFIX}{jti}")
    except Exception as e:
        # Fail closed: if we can't confirm the token wasn't revoked, deny access.
        logger.warning("JWT denylist check failed — rejecting token for safety: %s", e)
        raise HTTPException(status_code=503, detail="Could not verify token status") from e

    if revoked:
        raise HTTPException(status_code=401, detail="Token has been revoked")
