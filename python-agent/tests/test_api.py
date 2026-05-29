"""Integration-style tests for the POST /api/query endpoint.

All external dependencies (JWT auth, Gemini, ClickHouse) are patched so
these tests run without any live services.
"""
from unittest.mock import patch

import pytest
from fastapi.testclient import TestClient

from tests.conftest import TEST_USER_ID, make_token

# ── helpers ───────────────────────────────────────────────────────────────────

_VALID_SQL = f"SELECT count() AS c FROM shortener.clicks WHERE user_id = '{TEST_USER_ID}' LIMIT 10;"
_CH_RESULT = {"columns": ["c"], "data": [[42]], "row_count": 1}

_AUTH_OK = "app.main.get_user_id_from_request"
_GEN_SQL = "app.main.generate_sql"
_VAL_SQL = "app.main.validate_sql"
_EXEC_SQL = "app.main.execute_query"


def _auth_patch(return_value=TEST_USER_ID):
    """Patch get_user_id_from_request to return a fixed user_id (or raise)."""
    import asyncio
    from unittest.mock import AsyncMock
    return patch(_AUTH_OK, new=AsyncMock(return_value=return_value))


# ── health ────────────────────────────────────────────────────────────────────

def test_health_endpoint(client: TestClient):
    resp = client.get("/health")
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok"}


# ── /api/query happy path ─────────────────────────────────────────────────────

def test_query_success(client: TestClient):
    with _auth_patch(), \
         patch(_GEN_SQL, return_value=(_VALID_SQL, "Counts all clicks")), \
         patch(_VAL_SQL, return_value=(True, _VALID_SQL, "")), \
         patch(_EXEC_SQL, return_value=_CH_RESULT):

        resp = client.post(
            "/api/query",
            json={"question": "how many clicks did I get?"},
            headers={"Authorization": f"Bearer {make_token()}"},
        )

    assert resp.status_code == 200
    body = resp.json()
    assert body["question"] == "how many clicks did I get?"
    assert body["sql"] == _VALID_SQL
    assert body["row_count"] == 1
    assert body["data"] == [[42]]


# ── authentication failures ───────────────────────────────────────────────────

def test_query_unauthenticated_returns_401(client: TestClient):
    from fastapi import HTTPException
    from unittest.mock import AsyncMock
    with patch(_AUTH_OK, new=AsyncMock(side_effect=HTTPException(status_code=401, detail="Missing token"))):
        resp = client.post(
            "/api/query",
            json={"question": "how many clicks?"},
            headers={"Authorization": "Bearer invalid-token"},
        )
    assert resp.status_code == 401


# ── off-topic question ────────────────────────────────────────────────────────

def test_query_off_topic_returns_400(client: TestClient):
    with _auth_patch(), \
         patch(_GEN_SQL, return_value=("NOT_RELATED", "")):

        resp = client.post(
            "/api/query",
            json={"question": "write me a poem"},
            headers={"Authorization": f"Bearer {make_token()}"},
        )

    assert resp.status_code == 400
    assert "not about your link analytics" in resp.json()["detail"].lower()


# ── SQL generation failure ────────────────────────────────────────────────────

def test_query_sql_generation_empty_returns_400(client: TestClient):
    with _auth_patch(), \
         patch(_GEN_SQL, return_value=("", "could not process")):

        resp = client.post(
            "/api/query",
            json={"question": "something that confuses the model"},
            headers={"Authorization": f"Bearer {make_token()}"},
        )

    assert resp.status_code == 400
    assert "generate" in resp.json()["detail"].lower()


# ── SQL validation failure ────────────────────────────────────────────────────

def test_query_sql_validation_fails_returns_400(client: TestClient):
    with _auth_patch(), \
         patch(_GEN_SQL, return_value=("DROP TABLE clicks", "dangerous")), \
         patch(_VAL_SQL, return_value=(False, "", "Forbidden SQL pattern detected")):

        resp = client.post(
            "/api/query",
            json={"question": "delete all my data"},
            headers={"Authorization": f"Bearer {make_token()}"},
        )

    assert resp.status_code == 400
    assert "safety" in resp.json()["detail"].lower()


# ── ClickHouse error ──────────────────────────────────────────────────────────

def test_query_clickhouse_error_returns_500(client: TestClient):
    with _auth_patch(), \
         patch(_GEN_SQL, return_value=(_VALID_SQL, "explanation")), \
         patch(_VAL_SQL, return_value=(True, _VALID_SQL, "")), \
         patch(_EXEC_SQL, side_effect=Exception("ClickHouse connection refused")):

        resp = client.post(
            "/api/query",
            json={"question": "how many clicks?"},
            headers={"Authorization": f"Bearer {make_token()}"},
        )

    assert resp.status_code == 500
    assert "execution failed" in resp.json()["detail"].lower()


# ── request validation ────────────────────────────────────────────────────────

def test_query_question_too_short_returns_422(client: TestClient):
    with _auth_patch():
        resp = client.post(
            "/api/query",
            json={"question": "hi"},  # < 3 chars
            headers={"Authorization": f"Bearer {make_token()}"},
        )
    assert resp.status_code == 422


def test_query_missing_question_field_returns_422(client: TestClient):
    with _auth_patch():
        resp = client.post(
            "/api/query",
            json={},
            headers={"Authorization": f"Bearer {make_token()}"},
        )
    assert resp.status_code == 422
