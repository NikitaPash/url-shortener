import redis.asyncio as aioredis

from app.config import settings

_SOCKET_TIMEOUT_SECONDS = 2

_client: aioredis.Redis | None = None


def get_redis() -> aioredis.Redis:
    """Lazy-initialize a shared async Redis client."""
    global _client
    if _client is None:
        host, _, port = settings.redis_addr.partition(":")
        _client = aioredis.Redis(
            host=host,
            port=int(port) if port else 6379,
            password=settings.redis_password or None,
            db=settings.redis_db,
            socket_timeout=_SOCKET_TIMEOUT_SECONDS,
            socket_connect_timeout=_SOCKET_TIMEOUT_SECONDS,
            decode_responses=True,
        )
    return _client


async def close_redis() -> None:
    global _client
    if _client is not None:
        await _client.aclose()
        _client = None
