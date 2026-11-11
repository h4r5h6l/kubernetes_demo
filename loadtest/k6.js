// k6 load test for the Task Manager API.
//
// Run through the Ingress so traffic is load-balanced across all HPA replicas:
//   kubectl -n ingress-nginx port-forward svc/ingress-nginx-controller 8082:80
//   k6 run -e BASE_URL=http://taskmanager.local:8082 \
//     -e INGRESS_HOST=taskmanager.local loadtest/k6.js
//
// INGRESS_HOST makes k6 resolve the Ingress hostname to the local port-forward.
// A plain `port-forward svc/taskmanager` targets a single pod, so it is fine for
// smoke tests but does NOT exercise HPA scale-out.
//
// Every iteration performs a full CRUD cycle (create -> read -> update ->
// delete) so the requests exercise the HTTP layer, validation, SQL and the
// connection pool, and the tasks table does not grow without bound.

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8081';
// When set, resolve the Ingress hostname to the local port-forward address.
const INGRESS_HOST = __ENV.INGRESS_HOST || '';
const jsonHeaders = { 'Content-Type': 'application/json' };

const createDuration = new Trend('task_create_duration', true);
const readDuration = new Trend('task_read_duration', true);
const writeDuration = new Trend('task_write_duration', true);
const taskErrors = new Rate('task_errors');

// PROFILE=smoke|steady|stress selects the load shape. Default: stress.
const profiles = {
  smoke: {
    executor: 'constant-vus',
    vus: 5,
    duration: '30s',
  },
  steady: {
    executor: 'constant-vus',
    vus: 30,
    duration: '2m',
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
  },
};

const profile = __ENV.PROFILE || 'stress';
if (!profiles[profile]) {
  throw new Error(`unknown PROFILE "${profile}" (use smoke, steady or stress)`);
}

export const options = {
  hosts: INGRESS_HOST ? { [INGRESS_HOST]: '127.0.0.1' } : {},
  scenarios: { [profile]: profiles[profile] },
  thresholds: {
    http_req_failed: ['rate<0.01'], // < 1% failed requests
    http_req_duration: ['p(95)<500'], // p95 below 500 ms
    checks: ['rate>0.99'],
    task_errors: ['rate<0.01'],
  },
};

export default function () {
  const title = `k6 vu=${__VU} iter=${__ITER} ${Date.now()}`;

  const createRes = http.post(
    `${BASE_URL}/api/v1/tasks`,
    JSON.stringify({ title, description: 'k6 load test' }),
    { headers: jsonHeaders, tags: { endpoint: 'create' } },
  );
  createDuration.add(createRes.timings.duration);
  const created = check(createRes, { 'create returns 201': (r) => r.status === 201 });
  taskErrors.add(!created);
  if (!created) {
    return;
  }

  const id = createRes.json('id');

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

  const deleteRes = http.del(`${BASE_URL}/api/v1/tasks/${id}`, null, {
    tags: { endpoint: 'delete' },
  });
  writeDuration.add(deleteRes.timings.duration);
  taskErrors.add(!check(deleteRes, { 'delete returns 204': (r) => r.status === 204 }));

  sleep(0.2);
}
