// redirect_capacity.js — Exp 1: how many redirects/s does one node sustain, and
// where is the latency knee? This produces the headline capacity number.
//
// Open-loop: a fixed ARRIVAL RATE (ramping-arrival-rate), independent of how fast
// the server responds, ramped until the tail latency blows up or errors appear.
// This is the coordinated-omission-correct way to find capacity (see §1c of the
// plan) — unlike a closed-loop "N workers" model, a slowing server does NOT slow
// the offered load, so the reported tail is honest.
//
// Run:
//   docker compose -f docker-compose.yml -f docker-compose.loadtest.yml \
//     run --rm k6 run /scripts/scenarios/redirect_capacity.js
// Override the ramp:  -e STAGES="500:1m,1000:1m,2000:1m,4000:2m"

import http from 'k6/http';
import { check } from 'k6';
import { Trend, Counter } from 'k6/metrics';
import { cfg, ipHeaders, randItem, seedLinks, buildSummary } from '../lib/common.js';

const redirectLatency = new Trend('redirect_latency', true); // ms, our SLO metric
const rateLimited = new Counter('rate_limited_429');
const notFound = new Counter('not_found_404');

// STAGES="rate:dur,rate:dur,..." -> [{ target, duration }] for ramping-arrival-rate.
function parseStages(spec) {
  if (!spec) {
    return [
      { target: 500, duration: '1m' },
      { target: 1000, duration: '1m' },
      { target: 2000, duration: '1m' },
      { target: 4000, duration: '1m' },
      { target: 6000, duration: '1m' },
    ];
  }
  return spec.split(',').map((s) => {
    const [target, duration] = s.split(':');
    return { target: Number(target), duration };
  });
}

const stages = parseStages(__ENV.STAGES);
const maxRate = stages.reduce((mx, s) => Math.max(mx, s.target), 0);

export const options = {
  // Drop response bodies: a 302 has none worth keeping, and it keeps the load
  // generator cheap so the LG never becomes the bottleneck.
  discardResponseBodies: true,
  summaryTrendStats: ['min', 'med', 'avg', 'p(90)', 'p(95)', 'p(99)', 'max'],
  scenarios: {
    capacity: {
      executor: 'ramping-arrival-rate',
      startRate: Number(__ENV.START_RATE || stages[0].target),
      timeUnit: '1s',
      // Pre-allocate generously and allow many VUs: at high rates with non-trivial
      // latency, k6 needs enough VUs to keep issuing requests on schedule. If
      // dropped_iterations > 0, raise MAX_VUS (or the LG is saturated — add a VM).
      preAllocatedVUs: Number(__ENV.PRE_VUS || Math.min(maxRate, 2000)),
      maxVUs: Number(__ENV.MAX_VUS || Math.max(maxRate * 2, 4000)),
      stages,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],   // < 1% errors
    redirect_latency: ['p(99)<50'],   // SLO: p99 under 50 ms (the "knee" threshold)
  },
};

export function setup() {
  return seedLinks();
}

export default function (data) {
  const id = randItem(data.shortIds);
  const res = http.get(`${cfg.base}/${id}`, {
    headers: ipHeaders(),
    redirects: 0, // treat the 302 as the final response — don't chase the target URL
    tags: { endpoint: 'redirect' },
  });
  redirectLatency.add(res.timings.duration);
  if (res.status === 429) rateLimited.add(1);
  else if (res.status === 404) notFound.add(1);
  check(res, { 'redirect 302': (r) => r.status === 302 });
}

export function handleSummary(data) {
  return buildSummary(data, 'redirect_capacity');
}