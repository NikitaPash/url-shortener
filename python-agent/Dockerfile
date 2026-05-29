FROM python:3.12-slim

# Bring in the uv binary (pinned for reproducible builds).
COPY --from=ghcr.io/astral-sh/uv:0.9.30 /uv /uvx /bin/

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    # Compile to .pyc on install for faster cold starts; copy (don't hardlink)
    # out of the cache mount since it lives on a different filesystem.
    UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    # Run the app straight from the project venv.
    PATH="/app/.venv/bin:$PATH"

RUN useradd -m -u 10001 appuser

WORKDIR /app

# Install runtime deps only (no dev group, no project) in a cached layer that
# is rebuilt only when the lockfile or manifest change.
COPY pyproject.toml uv.lock ./
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-dev --no-install-project

COPY app/ ./app/

USER appuser

EXPOSE 8090 9093

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8090"]
