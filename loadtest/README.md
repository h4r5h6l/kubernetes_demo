# Load testing

`k6.js` is the load-test script. The acceptance profile is **`load500`**: 500
concurrent virtual users (VUs) for 2 minutes, which satisfies the requirement of
at least 500 concurrent users for at least 60 seconds.

Results and raw output from the acceptance run are committed next to the script:

| File | Contents |
| --- | --- |
| `load500-2m.txt` | Full k6 console output for the accepted run |
| `load500-summary.json` | Structured k6 metrics (machine-readable) |
| `hpa-during-load500.txt` | HPA utilization/replica trace captured during the run |

## Profiles

| `PROFILE` | VUs | Duration | Executor | Workload |
| --- | ---: | ---: | --- | --- |
| `smoke` | 5 | 30 s | constant | Full CRUD cycle |
| `steady` | 30 | 2 min | constant | Full CRUD cycle |
| `stress` | 0 → 50 | 4 min | ramping | Full CRUD cycle |
| `load500` | 500 | 2 min | constant | Read-mostly (default) |

The full CRUD cycle is create → get → list → update → delete, so the tasks table
does not grow. The 500-VU profile is read-mostly: every iteration lists tasks,
and every 20th iteration creates and deletes one task. A create/delete on every
iteration at 500 VUs would make PostgreSQL writes and the per-pod pgx pool
(`MaxConns=10`) the bottleneck rather than the HTTP path. The 1-in-20 write keeps
the database path exercised without flooding it.

Both CRUD and load scenarios always call `sleep()`, including when a create
fails. This is deliberate: the earlier baseline returned early on failure and
produced impossible throughput.

## Prerequisites

- The app image is built and loaded into Minikube:
  `make k8s-up` (or `make k8s-image`).
- `metrics-server` is enabled so the HPA has CPU data:
  `minikube addons enable metrics-server`.
- The HPA is applied. `k8s/hpa.yaml` is listed in `k8s/kustomization.yaml`, so
  `kubectl apply -k k8s/` creates it.

Verify before testing:

```bash
kubectl -n taskmanager get deploy taskmanager   # 1/1 ready
kubectl -n taskmanager get hpa taskmanager      # TARGETS should not be <unknown>
kubectl top pods -n taskmanager                 # CPU/memory values present
```

`TARGETS: <unknown>` for the first 15–60 seconds after a rollout is normal: the
metrics API has no sample for the new pod yet.

## Run the acceptance profile

The preferred path is through the `nginx` Ingress controller. Unlike a Service
`port-forward`, the Ingress distributes requests across all HPA replicas.

```bash
# One-time: enable the controller and metrics-server
minikube addons enable ingress
minikube addons enable metrics-server

# Build/load the image and apply the manifests (including the HPA)
make k8s-up

# Forward the Ingress controller's HTTP port
kubectl -n ingress-nginx port-forward svc/ingress-nginx-controller 8082:80 &

# Run 500 VUs for 2 minutes
PROFILE=load500 \
BASE_URL=http://taskmanager.local:8082 \
INGRESS_HOST=taskmanager.local \
k6 run loadtest/k6.js
```

`INGRESS_HOST` tells k6 to resolve `taskmanager.local` to `127.0.0.1`, so the
request carries the `Host: taskmanager.local` header and the Ingress routes it
to the `taskmanager` Service. This is why the URL uses the hostname even though
the connection goes to the local port-forward.

Save the raw output and metrics:

```bash
PROFILE=load500 \
BASE_URL=http://taskmanager.local:8082 \
INGRESS_HOST=taskmanager.local \
k6 run --summary-export=loadtest/load500-summary.json loadtest/k6.js \
  | tee loadtest/load500-2m.txt
```

The default `PROFILE` is already `load500`, so `PROFILE=...` can be omitted.

### Smaller runs

For a quick CRUD smoke test, the Service `port-forward` is sufficient. It targets
one pod, so do not use it for the 500-VU acceptance run.

```bash
kubectl -n taskmanager port-forward svc/taskmanager 8081:80 &
PROFILE=smoke BASE_URL=http://127.0.0.1:8081 k6 run loadtest/k6.js
```

`SLEEP` (CRUD think time, default `0.2` s) and `LOAD_SLEEP` (500-VU think time,
default `1` s) are optional environment overrides.

## Accepted result

Environment: Minikube, Docker driver, containerd runtime, 8 CPUs; `nginx`
Ingress controller; metrics-server; one app Deployment (HPA min 1, max 5).

| Metric | Result |
| --- | ---: |
| Concurrent VUs | **500** (max `vus_max = 500`) |
| Duration | **2 min** |
| HTTP requests | **61,063** |
| Throughput | **504.6 req/s** |
| Completed iterations | 55,063 (455.1 iter/s) |
| Failed requests / failed checks | **0 / 0** (0.00%) |
| Checks succeeded | **61,063 / 61,063 (100%)** |
| p90 latency | 299.5 ms |
| p95 latency | **518.1 ms** |
| Max latency | 1.68 s |
| Thresholds | **All passed** (`p95 < 2000 ms`, failure rate `< 5%`, checks `> 95%`) |

The 500-VU thresholds are deliberately looser than the smoke/steady thresholds.
A single-node Minikube shares CPU with the ingress controller, PostgreSQL,
metrics-server, and the cluster control plane; the acceptance criterion here is
that 500 concurrent users are sustained with no HTTP failures, not that p95
matches an idle single-client run.

Thresholds from the run:

```text
checks              ✓ 'rate>0.95'      rate=100.00%
http_req_duration   ✓ 'p(95)<2000'     p(95)=518.07ms
http_req_failed     ✓ 'rate<0.05'      rate=0.00%
task_errors         ✓ 'rate<0.05'      rate=0.00%
```

## HPA behaviour

The Deployment requests `200m` CPU and limits `500m`. The HPA targets **70% of
the request**, i.e. `140m` per pod, with `minReplicas: 1` and `maxReplicas: 5`.
The trace excerpt below is from `hpa-during-load500.txt`:

```text
20:35:51 hpa_util=15  hpa_current=1 hpa_desired=1 pods=1 ready=1
20:35:57 hpa_util=145 hpa_current=1 hpa_desired=3 pods=3 ready=1
20:36:03 hpa_util=145 hpa_current=1 hpa_desired=3 pods=3 ready=3
20:36:15 hpa_util=145 hpa_current=3 hpa_desired=3 pods=3 ready=3
20:36:56 hpa_util=145 hpa_current=3 hpa_desired=3 pods=3 ready=3
```

Under load the HPA moved from 1 to 3 desired replicas, and all 3 pods became
ready about 6 seconds after the scale-up. It did not need to reach the configured
maximum of 5 during this run.

`145%` is not a violation of the `70%` target. HPA utilization is
`usage / request`; the target is a control setpoint, not a CPU cap. A pod using
`290m` against a `200m` request reports `145%`, and the HPA reacts by adding
replicas. Metrics are also sampled periodically, so the displayed value can stay
above the target while new pods start and while the metrics-server window catches
up. See `k8s/hpa.yaml` for the scale-up/scale-down policies.

After the run, the `120s` scale-down stabilization window and the
`1 pod / 60s` scale-down policy return the Deployment to one replica.

> **Important:** while an HPA exists it owns the Deployment's `spec.replicas`.
> The manifest still declares `replicas: 1`, but the live value can temporarily
> be 2–5 during or just after a load test. For a strict graded single-replica
> state, delete the HPA and re-apply the base manifests. If `hpa.yaml` remains in
> `k8s/kustomization.yaml`, remove it there first; otherwise `kubectl apply -k`
> recreates the HPA.

```bash
kubectl delete -f k8s/hpa.yaml
# remove "- hpa.yaml" from k8s/kustomization.yaml for a permanent single-replica state
kubectl apply -k k8s/
kubectl -n taskmanager get deploy taskmanager -o jsonpath='{.spec.replicas}{"\n"}'  # 1
```

## Discarded baseline

An earlier 30-VU steady run was discarded and is not committed. It was not valid
evidence:

- It was truncated at about 45 seconds of a 2-minute profile.
- It reported roughly 11,800 iterations/s from 30 VUs. With a 0.2 s think time,
  30 VUs cannot exceed about 150 iterations/s.
- Cause: on a failed create, the old script returned before `sleep()`. Each VU
  looped on a fast failure, inflating throughput and completing no useful work.

The script now always sleeps, and the valid acceptance run is `load500`. The old
result was removed rather than presented as a baseline.
