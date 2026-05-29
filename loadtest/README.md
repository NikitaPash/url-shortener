# Load testing & feature showcase

Two scripts that exercise the running stack from the outside, plus a shared
helper module. Both are written to be presentable in a bachelor's-thesis report.

| Script | Purpose | Output |
|---|---|---|
| [`benchmark.py`](benchmark.py) | **Performance / stress test** — drives the API under load, measures latency percentiles and throughput, and runs a concurrency-scaling sweep. | JSON + Markdown + PNG charts in `results/` |
| [`showcase.py`](showcase.py) | **Guided feature demo** — walks every feature end-to-end, asserting expected behaviour, and seeds realistic data for the UI / Grafana. | Console transcript + Markdown report in `results/` |
| [`common.py`](common.py) | Shared traffic data, a typed API client, stats & console helpers. | — |

All generated artifacts land in `loadtest/results/`, which is **git-ignored**
(see `loadtest/.gitignore` and the root `.gitignore`). It is also outside every
Docker build context — the images build from `backend/shortener/`,
`clickhouse-consumer/`, `python-agent/` and `frontend/`, so nothing under
`loadtest/` is ever copied into an image.

---

## Prerequisites

- The stack running locally: `docker compose up -d --build`
- [uv](https://docs.astral.sh/uv/) (already used by the Python agent). Each script
  declares its own dependencies inline (PEP 723), so `uv run` provisions them in
  an ephemeral environment — no manual installs, no `requirements.txt`.

If you prefer plain Python, install the deps yourself: `httpx` for both, plus
`matplotlib` for the benchmark's charts.

---

## Performance benchmark

```bash
uv run loadtest/benchmark.py                       # sensible defaults (~2 min)
uv run loadtest/benchmark.py --duration 15 --concurrency 100
uv run loadtest/benchmark.py --scenarios redirect,shorten --no-sweep
uv run loadtest/benchmark.py --sweep-concurrency 1,4,16,64,256 --label run1
```

What it does:

1. Creates a dedicated benchmark account, seeds a handful of links, and warms the
   redirect cache so the measurements reflect steady state.
2. **Per-endpoint load** at a fixed concurrency for a fixed window (warmup
   excluded): `redirect` (cached hot path), `shorten` (write path), `list_links`,
   and `analytics` (heavier ClickHouse read). Reports throughput, success/error
   counts, and p50/p90/p95/p99/max latency.
3. **Scalability sweep** of the redirect hot path across concurrency levels to
   show the throughput/latency "knee".
4. Captures Prometheus counters across the run (redirects, Kafka publishes,
   dropped clicks, cache-hit ratio, server-side p95).

Each request rotates `X-Real-IP`, so the per-IP rate limiter does not throttle
the load — that is why the benchmark targets the Go API on `:8080` directly.

Output (`results/bench_<timestamp>[_label]/`):

- `results.json` — full machine-readable results (incl. raw latency samples)
- `summary.md` — report-ready tables + embedded charts
- `dashboard.png`, `throughput.png`, `latency_percentiles.png`,
  `latency_cdf.png`, `scalability_sweep.png`

Useful flags: `--duration`, `--warmup`, `--concurrency`, `--scenarios`,
`--seed-links`, `--warm-clicks`, `--no-sweep`, `--sweep-concurrency`,
`--sweep-duration`, `--label`, `--base`, `--gateway`.

> **Reading the numbers:** this is a *closed-loop* benchmark (N clients each
> issue requests back-to-back). Throughput is the achieved server rate at that
> concurrency; latency is the response-time distribution under that load. The
> sweep shows how both scale. Numbers depend on your machine — run it on the
> target host and cite that environment (it's recorded in `results.json`).

---

## Feature showcase

```bash
uv run loadtest/showcase.py                # full tour, 3000 demo clicks
uv run loadtest/showcase.py --clicks 8000  # richer analytics/Grafana data
uv run loadtest/showcase.py --no-ai        # skip the Gemini-backed section
uv run loadtest/showcase.py --clicks 0 --no-report
```

It runs ten labelled sections and prints a `✓/✗` transcript:

1. Health & readiness probes
2. Accounts & JWT auth (incl. the seeded admin and a wrong-password rejection)
3. Short-link creation — auto codes + custom aliases (collision-safe)
4. Listing & pagination
5. Realistic redirect traffic (weighted browser / device / country / referrer mix)
6. Link lifecycle — deactivate → `302 /app/not-found` → reactivate → delete
7. Per-link analytics API — period presets, custom range, bot exclusion, validation
8. AI agent — NL→SQL answers, an off-topic rejection, and a per-user **data-isolation** check
9. Security — per-IP rate limiting + JWT logout revocation (Redis denylist)
10. Observability — live Prometheus counters + the ClickHouse data landscape

At the end it prints login credentials for the demo accounts and writes a
Markdown report to `results/showcase_<timestamp>.md` (transcript, AI Q&A,
key figures, accounts) — drop it straight into an appendix.

To verify the admin login, export the same values as your `.env`:

```bash
ADMIN_EMAIL=admin@example.com ADMIN_PASSWORD=adminpassword uv run loadtest/showcase.py
```

The AI section needs a valid `GEMINI_API_KEY` in the stack's `.env`; without it
that section is skipped cleanly rather than failing.

---

## Configuration (environment variables)

Both scripts honour these (CLI flags override where present):

| Variable | Default | Meaning |
|---|---|---|
| `GO_API` | `http://localhost:8080` | Go API (targeted directly to bypass nginx's IP rewrite) |
| `GATEWAY` | `http://localhost` | nginx — used for the AI agent and UI links |
| `PROM_URL` | `http://localhost:9090` | Prometheus query API |
| `CLICKHOUSE_CONTAINER` | `shortener-clickhouse` | Container name for `docker exec` queries |
| `NO_COLOR` | _(unset)_ | Set to disable ANSI colour |
