import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException, Request
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from prometheus_client import start_http_server

from app.agent.gemini_client import generate_sql
from app.agent.prompt import UNRELATED_MARKER, build_prompt
from app.agent.sql_validator import validate_sql
from app.auth.jwt import get_user_id_from_request
from app.config import settings
from app.db.clickhouse import execute_query
from app.db.redis_client import close_redis
from app.models.schemas import QueryRequest, QueryResponse

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Tracing setup must happen at module level — middleware cannot be added after app starts.
_resource = Resource.create({"service.name": "python-agent"})
_provider = TracerProvider(resource=_resource)
_exporter = OTLPSpanExporter(endpoint=f"http://{settings.jaeger_endpoint}/v1/traces")
_provider.add_span_processor(BatchSpanProcessor(_exporter))
trace.set_tracer_provider(_provider)


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Prometheus metrics on separate port.
    start_http_server(settings.metrics_port)

    logger.info("AI Agent starting with telemetry")
    yield

    await close_redis()
    _provider.shutdown()
    logger.info("AI Agent shutting down")


app = FastAPI(
    title="URL Shortener AI Analytics Agent",
    description="Natural language queries over ClickHouse click analytics",
    version="1.0.0",
    lifespan=lifespan,
)

FastAPIInstrumentor.instrument_app(app)


@app.get("/health")
async def health():
    return {"status": "ok"}


@app.post("/api/query", response_model=QueryResponse)
async def query_analytics(body: QueryRequest, request: Request):
    """Accept a natural language question, generate SQL via Gemini,
    validate it, execute against ClickHouse, and return results."""

    # 1. Authenticate (verifies signature + expiry, and honors the logout denylist).
    # Any logged-in user may query analytics; the SQL is scoped to their own
    # user_id, so they only ever see their own clicks.
    user_id = await get_user_id_from_request(request)

    # 2. Build prompt.
    prompt = build_prompt(body.question, user_id)

    # 3. Generate SQL.
    sql, explanation = generate_sql(prompt)
    if not sql:
        logger.warning("SQL generation produced no query: %s", explanation)
        raise HTTPException(
            status_code=400,
            detail="Could not generate a query for that question.",
        )

    # 3a. Reject anything the model flagged as outside the analytics domain.
    if sql.strip().upper().startswith(UNRELATED_MARKER):
        logger.info("rejected off-topic question: %s", body.question)
        raise HTTPException(
            status_code=400,
            detail="That question is not about your link analytics.",
        )

    # 4. Validate.
    is_valid, cleaned_sql, error_msg = validate_sql(sql, user_id)
    if not is_valid:
        # Full detail (reason + offending SQL) is logged server-side only;
        # returning it would leak the schema and the validation rules.
        logger.warning("SQL validation failed: %s | SQL: %s", error_msg, sql)
        raise HTTPException(
            status_code=400,
            detail="The generated query was rejected by the safety checks.",
        )

    # 5. Execute (scoped to the authenticated user at the DB layer).
    try:
        result = execute_query(cleaned_sql, user_id)
    except Exception:
        logger.exception("ClickHouse query failed | SQL: %s", cleaned_sql)
        raise HTTPException(
            status_code=500,
            detail="Query execution failed.",
        )

    # 6. Return.
    return QueryResponse(
        question=body.question,
        sql=cleaned_sql,
        explanation=explanation,
        columns=result["columns"],
        data=result["data"],
        row_count=result["row_count"],
    )
