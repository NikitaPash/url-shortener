"""Unit tests for app.agent.gemini_client.generate_sql."""
from unittest.mock import MagicMock, patch

import pytest

from app.agent.gemini_client import _model, generate_sql


def _mock_response(text: str) -> MagicMock:
    m = MagicMock()
    m.text = text
    return m


# ── happy paths ──────────────────────────────────────────────────────────────

def test_returns_sql_on_first_line_and_explanation_on_second():
    response_text = (
        "SELECT count() FROM shortener.clicks WHERE user_id = 'u1' LIMIT 1\n"
        "Counts all clicks for the user today."
    )
    with patch.object(_model, "generate_content", return_value=_mock_response(response_text)):
        sql, explanation = generate_sql("how many clicks today?")

    assert sql.startswith("SELECT")
    assert "Counts" in explanation


def test_single_line_response_returns_empty_explanation():
    response_text = "SELECT count() FROM shortener.clicks WHERE user_id = 'u1' LIMIT 1"
    with patch.object(_model, "generate_content", return_value=_mock_response(response_text)):
        sql, explanation = generate_sql("count clicks")

    assert "SELECT" in sql
    assert explanation == ""


def test_explanation_prefix_is_stripped():
    response_text = (
        "SELECT short_id FROM shortener.clicks WHERE user_id = 'u1' LIMIT 10\n"
        "Explanation: Shows the top links clicked by the user."
    )
    with patch.object(_model, "generate_content", return_value=_mock_response(response_text)):
        _, explanation = generate_sql("top links")

    assert not explanation.lower().startswith("explanation:")
    assert "Shows" in explanation


def test_not_related_sentinel_passthrough():
    """The NOT_RELATED sentinel must be returned verbatim so main.py can detect it."""
    with patch.object(_model, "generate_content", return_value=_mock_response("NOT_RELATED")):
        sql, explanation = generate_sql("what is the weather like?")

    assert sql == "NOT_RELATED"
    assert explanation == ""


def test_leading_and_trailing_whitespace_stripped():
    response_text = "  SELECT 1 FROM shortener.clicks WHERE user_id = 'u1' LIMIT 1  \n  Some explanation.  "
    with patch.object(_model, "generate_content", return_value=_mock_response(response_text)):
        sql, explanation = generate_sql("test")

    assert not sql.startswith(" ")
    assert not explanation.endswith(" ")


# ── error paths ───────────────────────────────────────────────────────────────

def test_api_error_returns_empty_sql():
    with patch.object(_model, "generate_content", side_effect=Exception("API quota exceeded")):
        sql, explanation = generate_sql("any question")

    assert sql == ""
    assert explanation != ""  # fallback message is set


def test_api_error_does_not_raise():
    """The caller relies on (sql, explanation); exceptions must NOT propagate."""
    with patch.object(_model, "generate_content", side_effect=RuntimeError("network error")):
        result = generate_sql("anything")  # must not raise

    assert isinstance(result, tuple)
    assert len(result) == 2
