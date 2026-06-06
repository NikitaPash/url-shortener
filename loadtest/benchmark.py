#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = [
#   "httpx>=0.27",
#   "matplotlib>=3.8",
# ]
# ///
"""benchmark.py — performance & stress test for the URL-shortener stack.

Drives the Go API under load and produces a self-contained, report-ready set of
artifacts: latency percentiles, sustained throughput, a concurrency-scaling
curve, a Prometheus snapshot taken across the run, and PNG charts. Everything is
written under ``loadtest/results/`` (git/Docker-ignored).

It runs two complementary experiments:

1. **Per-endpoint load** — each scenario (redirect hot path, shorten write path,
   list-links, per-link analytics) is driven at a fixed concurrency for a fixed
   wall-clock window. Closed-loop model: N workers issue requests back-to-back,
   so throughput is the server's achieved rate and the latency distribution is
   the response time under that load. A short warmup is excluded from the stats.

2. **Scalability sweep** — the redirect hot path is replayed across a range of
   concurrency levels to show how throughput and tail latency scale (the classic
   "knee" curve), which is the headline result for a caching redirect service.

Each request rotates ``X-Real-IP`` so the per-IP rate limiter does not throttle
the load (see ``common`` for why we hit :8080 directly).

Usage (uv resolves dependencies automatically):

    uv run loadtest/benchmark.py
    uv run loadtest/benchmark.py --duration 15 --concurrency 100
    uv run loadtest/benchmark.py --scenarios redirect,shorten --no-sweep
    uv run loadtest/benchmark.py --sweep-concurrency 1,4,16,64,256 --label nginx

Plain Python works too if httpx + matplotlib are already installed.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import platform
import sys
import time
import uuid
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from time import perf_counter

import httpx

import common as cm

# Charts are optional: if matplotlib is missing we still emit JSON + markdown.
try:
    import matplotlib

    matplotlib.use("Agg")  # headless: render straight to PNG files
    import matplotlib.pyplot as plt

    _HAVE_MPL = True
except Exception:  # noqa: BLE001
    _HAVE_MPL = False

RESULTS_ROOT = Path(__file__).resolve().parent / "results"
MAX_LATENCY_SAMPLES = 20_000  # cap stored raw latencies per scenario

# ---------------------------------------------------------------------------
# Default target = the deployed PRODUCTION stack.
# ---------------------------------------------------------------------------
# The benchmark measures the *live* service the way a real remote client sees it
# — over TLS, through nginx — so it points at production by default, even when
# run from a dev laptop. In prod every component sits behind a single nginx
# origin, so one BASE_URL drives the API target, the gateway (AI agent/SPA) and
# Prometheus (proxied under /prometheus/). Override precedence:
#   --base / --gateway (CLI)  >  GO_API / GATEWAY / PROM_URL (env)  >  BASE_URL  >  this default.
# To hit the LOCAL stack instead (separate host ports per service), run with:
#   BASE_URL=http://localhost:8080 GATEWAY=http://localhost PROM_URL=http://localhost:9090
# NOTE: behind nginx the rotated X-Real-IP is overwritten with the real client
# IP, so the Go per-IP rate limiter (redirect 100/min, api 30/min) throttles a
# single benchmark client. A remote run therefore measures real per-request
# latency + the rate limiter, NOT raw server capacity. For a clean capacity
# number, drive the local stack on :8080 (where IP rotation works) or raise the
# RATE_LIMIT_* envs on the target first.
DEFAULT_BASE_URL = ""
BASE_URL = os.environ.get("BASE_URL", DEFAULT_BASE_URL).rstrip("/")


# ---------------------------------------------------------------------------
# Result containers
# ---------------------------------------------------------------------------
@dataclass
class PhaseResult:
    name: str
    concurrency: int
    window_s: float
    total: int = 0
    success: int = 0
    throttled: int = 0  # HTTP 429
    client_error: int = 0  # other 4xx
    server_error: int = 0  # 5xx
    exceptions: int = 0  # transport errors / timeouts
    status_counts: dict[int, int] = field(default_factory=dict)
    latencies_s: list[float] = field(default_factory=list)  # successful only

    @property
    def throughput_rps(self) -> float:
        return self.success / self.window_s if self.window_s > 0 else 0.0

    @property
    def attempted_rps(self) -> float:
        return self.total / self.window_s if self.window_s > 0 else 0.0

    @property
    def error_rate(self) -> float:
        bad = self.client_error + self.server_error + self.exceptions
        return bad / self.total if self.total else 0.0

    def latency_summary(self) -> dict[str, float]:
        return cm.summarize_latencies(self.latencies_s)


# A request builder is a zero-arg callable returning (method, url, kwargs) for
# httpx. It is called once per request, so headers (and thus X-Real-IP) are
# freshly randomised each time.


# ---------------------------------------------------------------------------
# Async load engine (closed loop)
# ---------------------------------------------------------------------------
async def run_phase(
    name: str,
    build_request,
    concurrency: int,
    duration: float,
    warmup: float,
    timeout: float = 15.0,
) -> PhaseResult:
    warmup = min(warmup, max(0.0, duration - 0.5))
    res = PhaseResult(name=name, concurrency=concurrency, window_s=duration - warmup)

    limits = httpx.Limits(
        max_connections=concurrency + 20,
        max_keepalive_connections=concurrency + 20,
    )
    loop = asyncio.get_event_loop()

    async with httpx.AsyncClient(
        timeout=timeout, follow_redirects=False, limits=limits
    ) as client:
        start = loop.time()
        warm_until = start + warmup
        deadline = start + duration

        async def worker() -> None:
            while loop.time() < deadline:
                method, url, kwargs = build_request()
                t0 = perf_counter()
                try:
                    resp = await client.request(method, url, **kwargs)
                    lat = perf_counter() - t0
                    status = resp.status_code
                    err = False
                except (httpx.HTTPError, OSError):
                    lat = perf_counter() - t0
                    status = 0
                    err = True

                if loop.time() < warm_until:
                    continue  # discard warmup samples

                res.total += 1
                res.status_counts[status] = res.status_counts.get(status, 0) + 1
                if err:
                    res.exceptions += 1
                elif status == 429:
                    res.throttled += 1
                elif status >= 500:
                    res.server_error += 1
                elif status >= 400:
                    res.client_error += 1
                else:
                    res.success += 1
                    if len(res.latencies_s) < MAX_LATENCY_SAMPLES:
                        res.latencies_s.append(lat)

        await asyncio.gather(*(asyncio.create_task(worker()) for _ in range(concurrency)))

    return res


# ---------------------------------------------------------------------------
# Setup: a dedicated benchmark user with seed links + a little click data
# ---------------------------------------------------------------------------
@dataclass
class Seed:
    token: str
    link_ids: list[str]
    analytics_available: bool


def setup(base_url: str, gateway: str, n_links: int, warm_clicks: int) -> Seed:
    cm.section("Setup")
    api = cm.ApiClient(base_url=base_url, gateway=gateway)
    suffix = uuid.uuid4().hex[:8]
    email = f"bench-{suffix}@loadtest.local"
    password = "benchmark-pass-123"
    token = api.register_and_login(email, password)
    cm.check(bool(token), f"benchmark account ready ({email})")

    link_ids: list[str] = []
    for url in cm.TARGET_URLS[:n_links]:
        try:
            link_ids.append(api.shorten_id(token, url))
        except httpx.HTTPError:
            pass
    cm.check(len(link_ids) > 0, f"seeded {len(link_ids)} links for the redirect/analytics paths")

    # Warm the redirect cache and give analytics something to read.
    if warm_clicks > 0 and link_ids:
        import random

        for _ in range(warm_clicks):
            api.redirect(random.choice(link_ids), headers=cm.click_headers())
        cm.info(f"generated {warm_clicks:,} warmup clicks (cache + analytics data)")

    analytics_available = True
    if link_ids:
        status, _ = api.analytics(token, link_ids[0], period=30)
        analytics_available = status != 503
        if not analytics_available:
            cm.warn("analytics endpoint returned 503 (ClickHouse reader not configured) — skipping that scenario")

    api.close()
    return Seed(token=token, link_ids=link_ids, analytics_available=analytics_available)


# ---------------------------------------------------------------------------
# Scenario request builders
# ---------------------------------------------------------------------------
def make_builders(base_url: str, seed: Seed):
    import random

    base = base_url.rstrip("/")
    auth = {"Authorization": f"Bearer {seed.token}"}

    def redirect():
        sid = random.choice(seed.link_ids)
        return "GET", f"{base}/{sid}", {"headers": cm.click_headers()}

    def shorten():
        body = {"url": f"https://example.com/bench/{uuid.uuid4().hex}"}
        headers = {**auth, "X-Real-IP": cm.rand_public_ip()}
        return "POST", f"{base}/api/shorten", {"headers": headers, "json": body}

    def list_links():
        headers = {**auth, "X-Real-IP": cm.rand_public_ip()}
        return "GET", f"{base}/api/links", {"headers": headers, "params": {"limit": 20}}

    def analytics():
        sid = random.choice(seed.link_ids)
        headers = {**auth, "X-Real-IP": cm.rand_public_ip()}
        return "GET", f"{base}/api/links/{sid}/analytics", {"headers": headers, "params": {"period": 30}}

    builders = {
        "redirect": redirect,
        "shorten": shorten,
        "list_links": list_links,
        "analytics": analytics,
    }
    if not seed.analytics_available:
        builders.pop("analytics")
    return builders


# ---------------------------------------------------------------------------
# Prometheus snapshot (counters captured before/after to derive deltas)
# ---------------------------------------------------------------------------
PROM_COUNTERS = {
    "redirects_total": "shortener_redirects_total",
    "kafka_published_total": "shortener_kafka_published_total",
    "clicks_dropped_total": "shortener_clicks_dropped_total",
    "cache_hits_total": "shortener_cache_hits_total",
    "cache_misses_total": "shortener_cache_misses_total",
}
PROM_P95 = 'histogram_quantile(0.95, sum(rate(http_server_request_duration_seconds_bucket{job="go-api"}[1m])) by (le))'


def prom_snapshot() -> dict[str, float]:
    return {k: cm.prom_float(expr) for k, expr in PROM_COUNTERS.items()}


def prom_delta(before: dict[str, float], after: dict[str, float]) -> dict[str, float]:
    delta = {k: max(0.0, after.get(k, 0) - before.get(k, 0)) for k in PROM_COUNTERS}
    hits, misses = delta["cache_hits_total"], delta["cache_misses_total"]
    delta["cache_hit_ratio"] = hits / (hits + misses) if (hits + misses) > 0 else 0.0
    delta["p95_latency_s"] = cm.prom_float(PROM_P95)
    return delta


# ---------------------------------------------------------------------------
# Charts
# ---------------------------------------------------------------------------
def _bar_labels(ax, bars, fmt="{:.0f}"):
    for b in bars:
        h = b.get_height()
        ax.annotate(fmt.format(h), (b.get_x() + b.get_width() / 2, h),
                    ha="center", va="bottom", fontsize=8)


def chart_percentiles(phases: list[PhaseResult], path: Path) -> None:
    names = [p.name for p in phases]
    p50 = [p.latency_summary()["p50"] for p in phases]
    p95 = [p.latency_summary()["p95"] for p in phases]
    p99 = [p.latency_summary()["p99"] for p in phases]
    x = range(len(names))
    w = 0.27
    fig, ax = plt.subplots(figsize=(9, 5))
    ax.bar([i - w for i in x], p50, w, label="p50", color="#3B82F6")
    ax.bar(list(x), p95, w, label="p95", color="#F59E0B")
    ax.bar([i + w for i in x], p99, w, label="p99", color="#EF4444")
    ax.set_xticks(list(x))
    ax.set_xticklabels(names)
    ax.set_ylabel("latency (ms)")
    ax.set_title("Response-time percentiles by endpoint")
    ax.legend()
    ax.grid(axis="y", alpha=0.3)
    fig.tight_layout()
    fig.savefig(path, dpi=130)
    plt.close(fig)


def chart_throughput(phases: list[PhaseResult], path: Path) -> None:
    names = [p.name for p in phases]
    rps = [p.throughput_rps for p in phases]
    fig, ax = plt.subplots(figsize=(9, 5))
    bars = ax.bar(names, rps, color="#10B981")
    _bar_labels(ax, bars)
    ax.set_ylabel("requests / second (successful)")
    ax.set_title("Sustained throughput by endpoint")
    ax.grid(axis="y", alpha=0.3)
    fig.tight_layout()
    fig.savefig(path, dpi=130)
    plt.close(fig)


def chart_cdf(phases: list[PhaseResult], path: Path) -> None:
    fig, ax = plt.subplots(figsize=(9, 5))
    for p in phases:
        if not p.latencies_s:
            continue
        ms = sorted(v * 1000 for v in p.latencies_s)
        ys = [i / len(ms) for i in range(1, len(ms) + 1)]
        ax.plot(ms, ys, label=p.name, linewidth=1.6)
    ax.set_xlabel("latency (ms)")
    ax.set_ylabel("cumulative fraction of requests")
    ax.set_title("Latency CDF")
    ax.set_ylim(0, 1)
    ax.legend()
    ax.grid(alpha=0.3)
    fig.tight_layout()
    fig.savefig(path, dpi=130)
    plt.close(fig)


def chart_sweep(sweep: dict, path: Path) -> None:
    pts = sweep["points"]
    conc = [p["concurrency"] for p in pts]
    rps = [p["throughput_rps"] for p in pts]
    p95 = [p["p95"] for p in pts]
    fig, ax1 = plt.subplots(figsize=(9, 5))
    ax1.plot(conc, rps, "o-", color="#10B981", label="throughput")
    ax1.set_xlabel("concurrency (parallel clients)")
    ax1.set_ylabel("throughput (req/s)", color="#10B981")
    ax1.tick_params(axis="y", labelcolor="#10B981")
    ax1.set_xscale("log", base=2)
    ax1.grid(alpha=0.3)
    ax2 = ax1.twinx()
    ax2.plot(conc, p95, "s--", color="#EF4444", label="p95 latency")
    ax2.set_ylabel("p95 latency (ms)", color="#EF4444")
    ax2.tick_params(axis="y", labelcolor="#EF4444")
    ax1.set_title(f"Scalability sweep — {sweep['scenario']} hot path")
    fig.tight_layout()
    fig.savefig(path, dpi=130)
    plt.close(fig)


def chart_dashboard(phases: list[PhaseResult], sweep: dict | None, path: Path) -> None:
    fig, axes = plt.subplots(2, 2, figsize=(14, 9))
    fig.suptitle("URL Shortener — Performance Dashboard", fontsize=15, fontweight="bold")

    # (0,0) throughput
    ax = axes[0][0]
    bars = ax.bar([p.name for p in phases], [p.throughput_rps for p in phases], color="#10B981")
    _bar_labels(ax, bars)
    ax.set_title("Throughput (req/s)")
    ax.grid(axis="y", alpha=0.3)

    # (0,1) percentiles
    ax = axes[0][1]
    names = [p.name for p in phases]
    x = range(len(names))
    w = 0.27
    ax.bar([i - w for i in x], [p.latency_summary()["p50"] for p in phases], w, label="p50", color="#3B82F6")
    ax.bar(list(x), [p.latency_summary()["p95"] for p in phases], w, label="p95", color="#F59E0B")
    ax.bar([i + w for i in x], [p.latency_summary()["p99"] for p in phases], w, label="p99", color="#EF4444")
    ax.set_xticks(list(x))
    ax.set_xticklabels(names)
    ax.set_title("Latency percentiles (ms)")
    ax.legend(fontsize=8)
    ax.grid(axis="y", alpha=0.3)

    # (1,0) CDF
    ax = axes[1][0]
    for p in phases:
        if not p.latencies_s:
            continue
        ms = sorted(v * 1000 for v in p.latencies_s)
        ys = [i / len(ms) for i in range(1, len(ms) + 1)]
        ax.plot(ms, ys, label=p.name, linewidth=1.5)
    ax.set_title("Latency CDF")
    ax.set_xlabel("ms")
    ax.set_ylim(0, 1)
    ax.legend(fontsize=8)
    ax.grid(alpha=0.3)

    # (1,1) sweep or histogram fallback
    ax = axes[1][1]
    if sweep and sweep["points"]:
        conc = [p["concurrency"] for p in sweep["points"]]
        rps = [p["throughput_rps"] for p in sweep["points"]]
        ax.plot(conc, rps, "o-", color="#10B981")
        ax.set_xscale("log", base=2)
        ax.set_title("Scalability: throughput vs concurrency")
        ax.set_xlabel("concurrency")
        ax.set_ylabel("req/s")
        ax.grid(alpha=0.3)
    else:
        hot = next((p for p in phases if p.latencies_s), None)
        if hot:
            ms = [v * 1000 for v in hot.latencies_s]
            clip = cm.percentile(sorted(ms), 0.99)
            ax.hist([m for m in ms if m <= clip], bins=40, color="#3B82F6", alpha=0.85)
            ax.set_title(f"{hot.name} latency histogram")
            ax.set_xlabel("ms")
    fig.tight_layout(rect=(0, 0, 1, 0.96))
    fig.savefig(path, dpi=130)
    plt.close(fig)


def render_charts(phases: list[PhaseResult], sweep: dict | None, out_dir: Path) -> list[str]:
    if not _HAVE_MPL:
        cm.warn("matplotlib not available — skipping charts (JSON + markdown still written)")
        return []
    made: list[str] = []
    chart_dashboard(phases, sweep, out_dir / "dashboard.png"); made.append("dashboard.png")
    chart_throughput(phases, out_dir / "throughput.png"); made.append("throughput.png")
    chart_percentiles(phases, out_dir / "latency_percentiles.png"); made.append("latency_percentiles.png")
    chart_cdf(phases, out_dir / "latency_cdf.png"); made.append("latency_cdf.png")
    if sweep and sweep["points"]:
        chart_sweep(sweep, out_dir / "scalability_sweep.png"); made.append("scalability_sweep.png")
    return made


# ---------------------------------------------------------------------------
# Persistence
# ---------------------------------------------------------------------------
def phase_to_dict(p: PhaseResult) -> dict:
    return {
        "name": p.name,
        "concurrency": p.concurrency,
        "window_s": round(p.window_s, 3),
        "total": p.total,
        "success": p.success,
        "throttled": p.throttled,
        "client_error": p.client_error,
        "server_error": p.server_error,
        "exceptions": p.exceptions,
        "error_rate": round(p.error_rate, 4),
        "throughput_rps": round(p.throughput_rps, 1),
        "attempted_rps": round(p.attempted_rps, 1),
        "status_counts": {str(k): v for k, v in sorted(p.status_counts.items())},
        "latency_ms": {k: round(v, 2) for k, v in p.latency_summary().items()},
        "latencies_sample_ms": [round(v * 1000, 3) for v in p.latencies_s],
    }


def write_results(out_dir: Path, payload: dict) -> None:
    (out_dir / "results.json").write_text(json.dumps(payload, indent=2), encoding="utf-8")


def write_summary_md(out_dir: Path, payload: dict, charts: list[str]) -> None:
    lines: list[str] = []
    lines.append(f"# Performance benchmark — {payload['started_at']}")
    if payload.get("label"):
        lines.append(f"\n**Label:** `{payload['label']}`")
    cfg = payload["config"]
    lines.append("\n## Configuration\n")
    lines.append(f"- Target: `{cfg['base_url']}`")
    lines.append(f"- Duration/scenario: {cfg['duration']}s (warmup {cfg['warmup']}s)")
    lines.append(f"- Concurrency: {cfg['concurrency']}")
    lines.append(f"- Seed links: {cfg['seed_links']}, warmup clicks: {cfg['warm_clicks']}")

    lines.append("\n## Per-endpoint results\n")
    lines.append("| Scenario | Conc | Throughput (req/s) | p50 | p95 | p99 | max | Success | Errors | 429 |")
    lines.append("|---|--:|--:|--:|--:|--:|--:|--:|--:|--:|")
    for p in payload["scenarios"]:
        lm = p["latency_ms"]
        lines.append(
            f"| {p['name']} | {p['concurrency']} | {p['throughput_rps']:,.0f} | "
            f"{lm['p50']:.1f} | {lm['p95']:.1f} | {lm['p99']:.1f} | {lm['max']:.1f} | "
            f"{p['success']:,} | {p['client_error'] + p['server_error'] + p['exceptions']:,} | {p['throttled']:,} |"
        )

    sweep = payload.get("sweep")
    if sweep and sweep["points"]:
        lines.append(f"\n## Scalability sweep — `{sweep['scenario']}`\n")
        lines.append("| Concurrency | Throughput (req/s) | p50 | p95 | p99 |")
        lines.append("|--:|--:|--:|--:|--:|")
        for pt in sweep["points"]:
            lines.append(
                f"| {pt['concurrency']} | {pt['throughput_rps']:,.0f} | "
                f"{pt['p50']:.1f} | {pt['p95']:.1f} | {pt['p99']:.1f} |"
            )

    prom = payload.get("prometheus_delta") or {}
    if prom:
        lines.append("\n## Prometheus (measured across the run)\n")
        lines.append(f"- Redirects served: **{prom.get('redirects_total', 0):,.0f}**")
        lines.append(f"- Kafka events published: **{prom.get('kafka_published_total', 0):,.0f}**")
        lines.append(f"- Clicks dropped (publish queue overflow): **{prom.get('clicks_dropped_total', 0):,.0f}**")
        lines.append(f"- Cache hit ratio: **{prom.get('cache_hit_ratio', 0) * 100:.1f}%**")
        lines.append(f"- p95 HTTP latency (server-side): **{prom.get('p95_latency_s', 0) * 1000:.1f} ms**")

    if charts:
        lines.append("\n## Charts\n")
        for c in charts:
            lines.append(f"![{c}]({c})")

    (out_dir / "summary.md").write_text("\n".join(lines) + "\n", encoding="utf-8")


# ---------------------------------------------------------------------------
# Console report
# ---------------------------------------------------------------------------
def print_phase_table(phases: list[PhaseResult]) -> None:
    cm.section("Results")
    header = f"{'scenario':<12}{'conc':>5}{'req/s':>10}{'p50':>9}{'p95':>9}{'p99':>9}{'max':>9}{'errors':>8}{'429':>6}"
    print("   " + cm.C.bold(header))
    print("   " + cm.C.dim("─" * len(header)))
    for p in phases:
        lm = p.latency_summary()
        errors = p.client_error + p.server_error + p.exceptions
        row = (
            f"{p.name:<12}{p.concurrency:>5}{p.throughput_rps:>10,.0f}"
            f"{lm['p50']:>8.1f}{lm['p95']:>8.1f}{lm['p99']:>8.1f}{lm['max']:>8.1f}"
            f"{errors:>8,}{p.throttled:>6,}"
        )
        print("   " + row)


# ---------------------------------------------------------------------------
# Orchestration
# ---------------------------------------------------------------------------
async def async_main(args) -> int:
    # Resolve the target: explicit CLI flag > per-service env > BASE_URL (prod default).
    base_url = args.base or os.environ.get("GO_API") or BASE_URL
    gateway = args.gateway or os.environ.get("GATEWAY") or BASE_URL
    # Point the Prometheus reader at the same origin (nginx proxies /prometheus/).
    cm.PROM_URL = os.environ.get("PROM_URL", f"{BASE_URL}/prometheus")

    cm.banner("URL Shortener — Performance Benchmark")
    cm.info(f"target {base_url}  |  prometheus {cm.PROM_URL}")
    cm.info(f"concurrency {args.concurrency}  |  {args.duration}s/scenario")

    probe = cm.ApiClient(base_url=base_url, gateway=gateway)
    if not probe.healthy():
        probe.close()
        print(cm.C.red(f"\n  go-api is not healthy at {base_url}. Is the stack up? (docker compose up -d)\n"))
        return 1
    probe.close()

    seed = setup(base_url, gateway, args.seed_links, args.warm_clicks)
    if not seed.link_ids:
        print(cm.C.red("\n  Could not create any seed links; aborting.\n"))
        return 1

    builders = make_builders(base_url, seed)
    requested = [s.strip() for s in args.scenarios.split(",") if s.strip()] if args.scenarios else list(builders)
    requested = [s for s in requested if s in builders]
    if not requested:
        print(cm.C.red("\n  No valid scenarios selected.\n"))
        return 1

    prom_before = prom_snapshot()

    # --- per-endpoint load --------------------------------------------------
    phases: list[PhaseResult] = []
    for name in requested:
        cm.section(f"Load: {name}  ({args.concurrency} clients × {args.duration}s)")
        pr = await run_phase(name, builders[name], args.concurrency, args.duration, args.warmup)
        phases.append(pr)
        lm = pr.latency_summary()
        cm.info(f"{pr.throughput_rps:,.0f} req/s · p95 {lm['p95']:.1f} ms · "
                f"{pr.success:,} ok / {pr.throttled:,} throttled / "
                f"{pr.client_error + pr.server_error + pr.exceptions:,} errors")

    # --- scalability sweep --------------------------------------------------
    sweep: dict | None = None
    if not args.no_sweep and "redirect" in builders:
        levels = [int(x) for x in args.sweep_concurrency.split(",") if x.strip()]
        cm.section(f"Scalability sweep: redirect @ {levels}")
        points = []
        for c in levels:
            pr = await run_phase("redirect", builders["redirect"], c, args.sweep_duration, min(1.0, args.sweep_duration / 4))
            lm = pr.latency_summary()
            points.append({
                "concurrency": c,
                "throughput_rps": round(pr.throughput_rps, 1),
                "success": pr.success,
                "p50": round(lm["p50"], 2),
                "p95": round(lm["p95"], 2),
                "p99": round(lm["p99"], 2),
            })
            cm.info(f"conc={c:<4} → {pr.throughput_rps:,.0f} req/s · p95 {lm['p95']:.1f} ms")
        sweep = {"scenario": "redirect", "concurrency_levels": levels, "points": points}

    prom_after = prom_snapshot()
    prom_d = prom_delta(prom_before, prom_after)

    # --- persist ------------------------------------------------------------
    started = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    dir_name = f"bench_{stamp}" + (f"_{args.label}" if args.label else "")
    out_dir = RESULTS_ROOT / dir_name
    out_dir.mkdir(parents=True, exist_ok=True)

    payload = {
        "tool": "benchmark",
        "schema_version": 1,
        "started_at": started,
        "label": args.label,
        "config": {
            "base_url": base_url,
            "gateway": gateway,
            "duration": args.duration,
            "warmup": args.warmup,
            "concurrency": args.concurrency,
            "seed_links": args.seed_links,
            "warm_clicks": args.warm_clicks,
            "scenarios": requested,
            "sweep_duration": args.sweep_duration,
        },
        "environment": {
            "python": sys.version.split()[0],
            "platform": platform.platform(),
            "processor": platform.processor(),
        },
        "scenarios": [phase_to_dict(p) for p in phases],
        "sweep": sweep,
        "prometheus_before": prom_before,
        "prometheus_after": prom_after,
        "prometheus_delta": prom_d,
    }
    write_results(out_dir, payload)
    charts = render_charts(phases, sweep, out_dir)
    write_summary_md(out_dir, payload, charts)

    # --- console ------------------------------------------------------------
    print_phase_table(phases)
    if prom_d.get("redirects_total"):
        cm.section("Prometheus (this run)")
        cm.info(f"redirects served:     {prom_d['redirects_total']:,.0f}")
        cm.info(f"kafka published:      {prom_d['kafka_published_total']:,.0f}")
        cm.info(f"clicks dropped:       {prom_d['clicks_dropped_total']:,.0f}")
        cm.info(f"cache hit ratio:      {prom_d['cache_hit_ratio'] * 100:.1f}%")
        cm.info(f"server-side p95:      {prom_d['p95_latency_s'] * 1000:.1f} ms")

    cm.banner("Done")
    rel = out_dir.relative_to(Path(__file__).resolve().parent.parent)
    print(f"   {cm.C.green('✓')} results written to {cm.C.bold(str(rel))}")
    for f in ["results.json", "summary.md", *charts]:
        print(f"      · {f}")
    print()
    return 0


def parse_args(argv: list[str]) -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Performance & stress test for the URL shortener.")
    p.add_argument("--base", default=None, help=f"API base URL (default BASE_URL={BASE_URL})")
    p.add_argument("--gateway", default=None, help=f"nginx gateway URL (default BASE_URL={BASE_URL})")
    p.add_argument("--duration", type=float, default=10.0, help="seconds per scenario (default 10)")
    p.add_argument("--warmup", type=float, default=2.0, help="warmup seconds excluded from stats (default 2)")
    p.add_argument("--concurrency", type=int, default=50, help="parallel clients per scenario (default 50)")
    p.add_argument("--scenarios", default="", help="comma list: redirect,shorten,list_links,analytics (default all)")
    p.add_argument("--seed-links", type=int, default=8, dest="seed_links", help="links to create for setup (default 8)")
    p.add_argument("--warm-clicks", type=int, default=1500, dest="warm_clicks", help="warmup clicks for cache/analytics (default 1500)")
    p.add_argument("--no-sweep", action="store_true", help="skip the concurrency scalability sweep")
    p.add_argument("--sweep-concurrency", default="1,2,4,8,16,32,64,128", dest="sweep_concurrency",
                   help="comma list of concurrency levels for the sweep")
    p.add_argument("--sweep-duration", type=float, default=5.0, dest="sweep_duration",
                   help="seconds per sweep step (default 5)")
    p.add_argument("--label", default="", help="optional label appended to the results dir name")
    return p.parse_args(argv)


def main() -> None:
    args = parse_args(sys.argv[1:])
    try:
        rc = asyncio.run(async_main(args))
    except KeyboardInterrupt:
        print("\nInterrupted.")
        rc = 130
    sys.exit(rc)


if __name__ == "__main__":
    main()
