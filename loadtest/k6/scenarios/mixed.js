// mixed.js — Exp 4: realistic traffic mix (and, with a long single stage, Exp 6
// soak). Drives all four hot endpoints at proportional arrival rates so the
// latency table reflects how the system behaves under a real workload rather than
// one isolated endpoint. Weights ~ 90% redirect / 5% shorten / 3% list / 2%
// analytics (a read-dominated shortener).
//
// Run:
//   docker compose -f docker-compose.yml -f docker-compose.loadtest.yml \
//     run --rm k6 run /scripts/scenarios/mixed.js
// Soak (Exp 6): -e TOTAL_RATE=400 -e DURATION=45m

import http from 'k6/http';
import { check } from 'k6';
import { Trend, Counter } from 'k6/metrics';
import { cfg, ipHeaders, authHeaders, randItem, seedLinks, buildSummary } from '../lib/common.js';

const redirectLatency = new Trend('redirect_latency', true);
const shortenLatency = new Trend('shorten_latency', true);
const listLatency = new Trend('list_latency', true);
const analyticsLatency = new Trend('analytics_latency', true);
const errors = new Counter('endpoint_errors');

const TOTAL = Number(__ENV.TOTAL_RATE || 500); // total req/s across all endpoints
const DURATION = __ENV.DURATION || '5m';
const JSON_HDR = { 'Content-Type': 'application/json' };

// Proportional arrival rates (rounded, min 1).
const rate = (frac) => Math.max(1, Math.round(TOTAL * frac));

function scenario(exec, frac) {
  const r = rate(frac);
  return {
    executor: 'constant-arrival-rate',
    exec,
    rate: r,
    timeUnit: '1s',
    duration: DURATION,
    preAllocatedVUs: Math.max(r * 2, 20),
    maxVUs: Math.max(r * 6, 100),
  };
}

export const options = {
  discardResponseBodies: true,
  summaryTrendStats: ['min', 'med', 'avg', 'p(90)', 'p(95)', 'p(99)', 'max'],
  scenarios: {
    redirect: scenario('redirect', 0.90),
    shorten: scenario('shorten', 0.05),
    list: scenario('list', 0.03),
    analytics: scenario('analytics', 0.02),
  },
  thresholds: {
    redirect_latency: ['p(99)<100'],
    http_req_failed: ['rate<0.02'],
  },
};

export function setup() {
  return seedLinks();
}

export function redirect(data) {
  const res = http.get(`${cfg.base}/${randItem(data.shortIds)}`, {
    headers: ipHeaders(),
    redirects: 0,
    tags: { endpoint: 'redirect' },
  });
  redirectLatency.add(res.timings.duration);
  if (!check(res, { 'redirect 302': (r) => r.status === 302 })) errors.add(1);
}

export function shorten(data) {
  const res = http.post(
    `${cfg.base}/api/shorten`,
    JSON.stringify({ url: 'https://example.com/landing-page' }),
    { headers: authHeaders(data.token, JSON_HDR), tags: { endpoint: 'shorten' } },
  );
  shortenLatency.add(res.timings.duration);
  if (!check(res, { 'shorten 2xx': (r) => r.status === 200 || r.status === 201 })) errors.add(1);
}

export function list(data) {
  const res = http.get(`${cfg.base}/api/links?limit=20&offset=0`, {
    headers: authHeaders(data.token),
    tags: { endpoint: 'list' },
  });
  listLatency.add(res.timings.duration);
  if (!check(res, { 'list 200': (r) => r.status === 200 })) errors.add(1);
}

export function analytics(data) {
  const res = http.get(`${cfg.base}/api/links/${randItem(data.shortIds)}/analytics`, {
    headers: authHeaders(data.token),
    tags: { endpoint: 'analytics' },
  });
  analyticsLatency.add(res.timings.duration);
  if (!check(res, { 'analytics 200': (r) => r.status === 200 })) errors.add(1);
}

export function handleSummary(data) {
  return buildSummary(data, 'mixed');
}
