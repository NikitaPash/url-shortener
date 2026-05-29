import logging

import google.generativeai as genai
from opentelemetry import trace

from app.config import settings

logger = logging.getLogger(__name__)
tracer = trace.get_tracer("agent.gemini")

genai.configure(api_key=settings.gemini_api_key)

_model = genai.GenerativeModel(settings.gemini_model)


def generate_sql(prompt: str) -> tuple[str, str]:
    """Send a prompt to Gemini and extract the SQL and explanation.

    Returns:
        (sql, explanation) — sql is empty string on failure.
    """
    with tracer.start_as_current_span("gemini.generate_sql") as span:
        span.set_attribute("prompt.length", len(prompt))

        try:
            response = _model.generate_content(
                prompt,
                generation_config=genai.GenerationConfig(
                    temperature=0.0,
                    max_output_tokens=512,
                    top_p=1.0,
                ),
            )

            text = response.text.strip()

            lines = text.split('\n', 1)
            sql = lines[0].strip()
            explanation = lines[1].strip() if len(lines) > 1 else ""

            if explanation.lower().startswith("explanation:"):
                explanation = explanation[len("explanation:"):].strip()

            span.set_attribute("sql.generated", sql[:200])
            return sql, explanation

        except Exception as e:
            span.record_exception(e)
            logger.exception("Gemini API error")
            return "", "the AI model could not process the request"
