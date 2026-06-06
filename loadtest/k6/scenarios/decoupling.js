// decoupling.js — Exp 2: the thesis-defining experiment.
//
// Claim: redirect latency is INVARIANT to analytics (click) volume, because the
// redirect publishes the click event fire-and-forget through an async, in-flight-
// capped Kafka writer (PublishClickAsync / maxInFlight) and never blocks on it.
//
// Design: two scenarios run at once against the same redirect endpoint —
//   * probe  — a LOW, constant arrival rate whose latency we MEASURE
//              (probe_redirect_latency). This is the SLO-sensitive signal.
//   * flood  — a HIGH, ramping arrival rate that exists only to FLOOD the
//              Kafka -> ClickHouse pipeline with click events.
// As the flood ramps, watch (in Grafana) shortener_clicks_dropped_total climb and
// Kafka lag grow — while the probe p99 stays FLAT. That flat line is the async
// pipeline decoupling the hot path from analytics. Without this architecture the
// probe latency would rise in lockstep with click volume.
//
// NOTE: the flood also adds raw redirect CPU load, so keep its peak within the
// node's redirect capacity (from Exp 1) — the point is to saturate the PIPELINE,
// not the CPU. For the cleaner "pipeline gone entirely" proof, see Exp 3
// (failure-injection: kill Kafka under steady load, redirects unaffected).
//
// Run:
//   docker compose -f docker-compose.yml -f docker-compose.loadtest.yml \
//     run --rm k6 run /scripts/scenarios/decoupling.js

import http from 'k6/http';
import { check } from 'k6';
import { Trend, Counter } from 'k6/metrics';
import { cfg, ipHeaders, randItem, seedLinks, buildSummary } from '../lib/common.js';

const probeLatency = new Trend('probe_redirect_latency', true); // the flat-line metric
const floodLatency = new Trend('flood_redirect_latency', true);
const rateLimited = new Counter('rate_limited_429');

const probeRate = Number(__ENV.PROBE_RATE || 50); // steady, low — the measured probe

export const options = {
  discardResponseBodies: true,
  summaryTrendStats: ['min', 'med', 'avg', 'p(90)', 'p(95)', 'p(99)', 'max'],
  scenarios: {
    // The probe: constant low rate for the whole run; its latency is the headline.
    probe: {
      executor: 'constant-arrival-rate',
      exec: 'probe',
      rate: probeRate,
      timeUnit: '1s',
      duration: '6m',
      preAllocatedVUs: Math.max(probeRate * 2, 50),
      maxVUs: Math.max(probeRate * 4, 200),
    },
    // The flood: ramps click volume through the pipeline. Stays idle for the first
    // 30s so we capture a clean "pipeline idle" baseline of the probe first.
    flood: {
      executor: 'ramping-arrival-rate',
      exec: 'flood',
      startRate: 0,
      timeUnit: '1s',
      preAllocatedVUs: 1000,
      maxVUs: 4000,
      stages: [
        { target: 0, duration: '30s' },    // baseline: probe alone
        { target: 1000, duration: '1m' },
        { target: 2500, duration: '2m' },
        { target: 2500, duration: '1m' },  // hold at peak ingestion
        { target: 0, duration: '30s' },    // recovery: probe alone again
      ],
    },
  },
  thresholds: {
    // The whole point: the probe tail must stay flat regardless of flood volume.
    probe_redirect_latency: ['p(99)<50'],
  },
};

export function setup() {
  return seedLinks();
}

function hit(data, trend) {
  const id = randItem(data.shortIds);
  const res = http.get(`${cfg.base}/${id}`, {
    headers: ipHeaders(),
    redirects: 0,
    tags: { endpoint: 'redirect' },
  });
  trend.add(res.timings.duration);
  if (res.status === 429) rateLimited.add(1);
  check(res, { 'redirect 302': (r) => r.status === 302 });
}

export function probe(data) {
  hit(data, probeLatency);
}
export function flood(data) {
  hit(data, floodLatency);
}

export function handleSummary(data) {
  return buildSummary(data, 'decoupling');
}