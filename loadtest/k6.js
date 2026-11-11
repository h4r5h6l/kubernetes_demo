// k6 load test for the Task Manager API.
//
// PROFILE=load500 is the acceptance profile: 500 concurrent VUs for 2 minutes.
// The assignment requires at least 500 concurrent users for at least 60 s.
//
// Recommended run (through the Ingress, so traffic is spread across all HPA
// replicas instead of a single pod):
//
//   kubectl -n ingress-nginx port-forward svc/ingress-nginx-controller 8082:80
//   PROFILE=load500 \
//     BASE_URL=http://taskmanager.local:8082 \
//     INGRESS_HOST=taskmanager.local \
//     k6 run loadtest/k6.js
//
// Small smoke run against the Service port-forward (single pod):
//
//   kubectl -n taskmanager port-forward svc/taskmanager 8081:80
//   PROFILE=smoke BASE_URL=http://127.0.0.1:8081 k6 run loadtest/k6.js
//
// INGRESS_HOST makes k6 resolve the Ingress hostname to INGRESS_IP
// (default 127.0.0.1 for a host port-forward). When running k6 inside the
// cluster, set INGRESS_IP to the Ingress controller ClusterIP.
//
// The CRUD scenarios run a full create -> read -> update -> delete cycle so
// the tasks table does not grow. The load500 scenario is read-mostly (list
// tasks, with one create+delete every 20th iteration) so 500 VUs exercise
// concurrency without turning the PostgreSQL write path into the bottleneck.

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8081';
const INGRESS_HOST = __ENV.INGRESS_HOST || '';
const INGRESS_IP = __ENV.INGRESS_IP || '127.0.0.1';
const jsonHeaders = { 'Content-Type': 'application/json' };

const createDuration = new Trend('task_create_duration', true);
const readDuration = new Trend('task_read_duration', true);
const writeDuration = new Trend('task_write_duration', true);
const taskErrors = new Rate('task_errors');

const profiles = {
  smoke: {
    executor: 'constant-vus',
    vus: 5,
    duration: '30s',
    exec: 'crud',
  },
  steady: {
    executor: 'constant-vus',
    vus: 30,
    duration: '2m',
    exec: 'crud',
  },
  stress: {
    executor: 'ramping-vus',
    startVUs: 0,
    stages: [
      { duration: '30s', target: 10 }, // warm up
      { duration: '1m', target: 50 },  // ramp
      { duration: '2m', target: 50 },  // steady load
      { duration: '30s', target: 0 },  // ramp down
    ],
    gracefulRampDown: '10s',
    exec: 'crud',
  },
  // Assignment acceptance profile: >=500 concurrent users for >=60 s.
  load500: {
    executor: 'constant-vus',
    vus: 500,
    duration: '2m',
    exec: 'load',
  },
};

const profile = __ENV.PROFILE || 'load500';
if (!profiles[profile]) {
  throw new Error(`unknown PROFILE "${profile}" (use smoke, steady, stress, load500)`);
}

const strictThresholds = {
  http_req_failed: ['rate<0.01'], // < 1% failed requests
  http_req_duration: ['p(95)<500'], // p95 below 500 ms
  checks: ['rate>0.99'],
  task_errors: ['rate<0.01'],
};

// 500 VUs on a local Minikube is CPU-bound; allow more latency headroom while
// still failing on real problems.
const loadThresholds = {
  http_req_failed: ['rate<0.05'], // < 5% failed requests
  http_req_duration: ['p(95)<2000'], // p95 below 2 s
  checks: ['rate>0.95'],
  task_errors: ['rate<0.05'],
};

export const options = {
  hosts: INGRESS_HOST ? { [INGRESS_HOST]: INGRESS_IP } : {},
  scenarios: { [profile]: profiles[profile] },
  thresholds: profile === 'load500' ? loadThresholds : strictThresholds,
};

function createTask(title) {
  const res = http.post(
    `${BASE_URL}/api/v1/tasks`,
    JSON.stringify({ title, description: 'k6 load test' }),
    { headers: jsonHeaders, tags: { endpoint: 'create' } },
  );
  createDuration.add(res.timings.duration);
  const ok = check(res, { 'create returns 201': (r) => r.status === 201 });
  taskErrors.add(!ok);
  return ok ? res.json('id') : null;
}

function deleteTask(id) {
  const res = http.del(`${BASE_URL}/api/v1/tasks/${id}`, null, {
    tags: { endpoint: 'delete' },
  });
  writeDuration.add(res.timings.duration);
  taskErrors.add(!check(res, { 'delete returns 204': (r) => r.status === 204 }));
}

// Full CRUD cycle. Used by the smoke/steady/stress profiles.
export function crud() {
  const title = `k6 crud vu=${__VU} iter=${__ITER} ${Date.now()}`;
  const id = createTask(title);

  if (id !== null) {
    const getRes = http.get(`${BASE_URL}/api/v1/tasks/${id}`, {
      tags: { endpoint: 'get' },
    });
    readDuration.add(getRes.timings.duration);
    taskErrors.add(!check(getRes, { 'get returns 200': (r) => r.status === 200 }));

    const listRes = http.get(`${BASE_URL}/api/v1/tasks`, {
      tags: { endpoint: 'list' },
    });
    readDuration.add(listRes.timings.duration);
    taskErrors.add(!check(listRes, { 'list returns 200': (r) => r.status === 200 }));

    const updateRes = http.put(
      `${BASE_URL}/api/v1/tasks/${id}`,
      JSON.stringify({ completed: true }),
      { headers: jsonHeaders, tags: { endpoint: 'update' } },
    );
    writeDuration.add(updateRes.timings.duration);
    taskErrors.add(!check(updateRes, { 'update returns 200': (r) => r.status === 200 }));

    deleteTask(id);
  }

  // Always sleep, including the create-failure path. An early return without
  // a sleep inflates iterations/s and made the earlier baseline meaningless.
  sleep(Number(__ENV.SLEEP || 0.2));
}

// Read-mostly scenario for 500 concurrent users.
export function load() {
  const listRes = http.get(`${BASE_URL}/api/v1/tasks`, {
    tags: { endpoint: 'list' },
  });
  readDuration.add(listRes.timings.duration);
  taskErrors.add(!check(listRes, { 'list returns 200': (r) => r.status === 200 }));

  // Keep a small write load on the SQL path: 1 in 20 iterations.
  if (__ITER % 20 === 0) {
    const id = createTask(`k6 load vu=${__VU} iter=${__ITER} ${Date.now()}`);
    if (id !== null) {
      deleteTask(id);
    }
  }

  sleep(Number(__ENV.LOAD_SLEEP || 1));
}
