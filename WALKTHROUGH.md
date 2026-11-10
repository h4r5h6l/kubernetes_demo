# Task Manager — Walkthrough & Testing Plan

This document is the guided path through the project: first a code/architecture
walkthrough, then CRUD verification at every layer (local, Compose, Kubernetes),
and finally the plan for load and crash-tolerance testing.

Work top to bottom. Each phase has commands, expected results, and checkboxes.

---

## 0. Current state

| Component | State |
| --- | --- |
| Local Postgres container | `taskmanager-postgres` running on `localhost:5432` |
| Local app | not running (`localhost:8080` is free) |
| Docker Compose stack | stopped (volume `kubernetes_demo_postgres_data` kept) |
| Minikube / Kubernetes app | running, 1 app replica + Postgres |
| Kubernetes access | port-forward active at **http://localhost:8081** |
| Automated tests | `go test ./...` and `go vet ./...` passing |

> Rule of thumb: only one stack may own port **8080** at a time.
> Kubernetes is exposed separately on **8081**.

---

## 1. Test surfaces

| Surface | URL | Notes |
| --- | --- | --- |
| Local API | `http://localhost:8080/tasks` | `make run` + local Postgres |
| Local UI | `http://localhost:8080/` | same server |
| API alias | `http://localhost:8080/api/v1/tasks` | same handlers |
| Compose API/UI | `http://localhost:8080` | `docker compose up` |
| Kubernetes API/UI | `http://localhost:8081` | `kubectl port-forward` |
| Liveness | `/health` | returns `200 {"status":"ok"}`; no DB access |
| Readiness | `/ready` | pings PostgreSQL: `200 {"status":"ready"}` or `503 {"status":"unavailable"}` |

---

## 2. Phase 1 — Codebase walkthrough (read-only, ~15 min)

Read these in order. The request path becomes obvious quickly.

| File | What to notice |
| --- | --- |
| `cmd/server/main.go` | config → DB retry → migrations → routes → HTTP server → graceful shutdown; `-healthcheck` flag |
| `internal/config/config.go` | env defaults, `.env` only outside production, port/URL validation |
| `internal/database/postgres.go` | pgxpool settings, ping, `ConnectWithRetry` (30 × 2s) |
| `internal/database/migrate.go` | embedded SQL, `schema_migrations`, advisory lock, per-migration transaction |
| `migrations/0001_create_tasks.sql` | `tasks` table + index |
| `internal/task/model.go` | `Task`, create/update inputs, rune-based validation |
| `internal/task/repository.go` | `Repository` interface + SQL implementation + `ErrNotFound` |
| `internal/task/handler_api.go` | JSON CRUD, partial updates, status codes, `Location` header |
| `internal/httpx/errors.go` | consistent `{"error":{...}}` envelope |
| `internal/httpx/health.go` | `/health` liveness without DB access; `/ready` pings the DB |
| `web/handler.go` | HTML CRUD, form parsing, validation re-render, redirects |
| `web/templates/` | layout + list/edit/error pages |
| `web/static/style.css` | embedded styling, no build step |
| `Dockerfile` | three stages: `build` (compile), `dev` (Air live reload), `production` (distroless non-root) |
| `.air.toml` | Air build/watch settings (polling enabled for bind mounts) |
| `docker-compose.yaml` | production stack: app + Postgres, named network/volume, healthchecks, `restart: unless-stopped` |
| `k8s/` | 1-replica Deployment, probes, limits, Postgres StatefulSet + PVC |

**Request lifecycle (API):**

```
curl → Gin router → task.Handler → task.Repository → pgxpool → PostgreSQL
                                 ↓
                       httpx error envelope / JSON task
```

**Startup lifecycle:**

```
config.Load → database.ConnectWithRetry → database.Migrate
            → register API/UI routes → listen :8080
            → SIGTERM → http.Server.Shutdown (10s) → pool.Close
```

Checklist:
- [ ] I can explain where a `POST /tasks` request is validated and persisted
- [ ] I can explain how migrations are applied exactly once
- [ ] I can explain why the container is static and distroless
- [ ] I can explain why the K8s Deployment has exactly 1 replica

---

## 3. Phase 2 — Automated checks

```bash
cd /home/hrshl/Documents/gitProjects/kubernetes_demo

make fmt
make vet
make test
```

Repository integration test (uses the local Postgres container):

```bash
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/tasks?sslmode=disable' \
  /usr/local/go/bin/go test ./internal/task -run TestPostgresRepositoryCRUD -v
```

Static binary checks:

```bash
make build
file bin/taskmanager
ldd bin/taskmanager 2>&1 || true
```

Expected:
- [ ] All tests pass
- [ ] Integration test runs (not skipped) and passes
- [ ] Binary says `statically linked` and `not a dynamic executable`

---

## 4. Phase 3 — Local API CRUD (curl)

### 4.1 Start the local stack

Terminal A:

```bash
cd /home/hrshl/Documents/gitProjects/kubernetes_demo
make run
```

Expected log line: `listening on :8080 (env=development)`.

Terminal B:

```bash
curl -sS -w '\nHTTP %{http_code}\n' http://localhost:8080/health
```

Expected: `{"status":"ok"}` and `HTTP 200`.

### 4.2 Full CRUD smoke script

Copy-paste as one block:

```bash
set -euo pipefail
BASE=http://localhost:8080

echo "== health =="
curl -sS -w '\nHTTP %{http_code}\n' "$BASE/health"

echo "== create =="
CREATED=$(curl -sS -X POST "$BASE/tasks" \
  -H 'Content-Type: application/json' \
  -d '{"title":"  Walkthrough task  ","description":"created via curl"}')
echo "$CREATED" | jq
ID=$(echo "$CREATED" | jq -r '.id')
test "$ID" != "null"

echo "== read all =="
curl -sS "$BASE/tasks" | jq

echo "== read one =="
curl -sS "$BASE/tasks/$ID" | jq

echo "== partial update =="
curl -sS -X PUT "$BASE/tasks/$ID" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Updated walkthrough task","completed":true}' | jq

echo "== alias /api/v1 =="
curl -sS "$BASE/api/v1/tasks/$ID" | jq '.id'

echo "== delete =="
curl -sS -o /dev/null -w 'HTTP %{http_code}\n' -X DELETE "$BASE/tasks/$ID"

echo "== delete again (expect 404) =="
curl -sS -w '\nHTTP %{http_code}\n' -X DELETE "$BASE/tasks/$ID"
```

Expected:
- Create → `201`, title trimmed to `Walkthrough task`, `Location: /tasks/<id>`
- List → JSON array including the task
- Get one → `200`, matching `id`
- Update → `200`, `completed: true`, description unchanged
- Alias → same id
- Delete → `204` with empty body
- Second delete → `404 not_found`

### 4.3 Negative / validation matrix

| Case | Command | Expected |
| --- | --- | --- |
| Missing title | `curl -sS -X POST $BASE/tasks -H 'Content-Type: application/json' -d '{}'` | `400`, `validation_error`, `fields.title` |
| Blank title | `... -d '{"title":"   "}'` | `400`, `fields.title` |
| Title > 200 chars | `LONG=$(printf 'a%.0s' {1..201}); ... -d "{\"title\":\"$LONG\"}"` | `400`, `fields.title` |
| Description > 2000 chars | same pattern with `description` | `400`, `fields.description` |
| Malformed JSON | `... -d '{"title":'` | `400`, `invalid_json` |
| Unknown id | `curl -i $BASE/tasks/999999` | `404`, `not_found` |
| Non-numeric id | `curl -i $BASE/tasks/abc` | `400`, `invalid_id` |
| Empty list | `curl -sS $BASE/tasks` | `[]` (never `null`) |

Checklist:
- [ ] All five CRUD operations verified
- [ ] Every negative case returns the documented status/code
- [ ] Response envelope is consistent

### 4.4 Cleanup

```bash
# Delete every task through the API (no direct DB access needed)
for id in $(curl -sS http://localhost:8080/tasks | jq -r '.[].id'); do
  curl -sS -o /dev/null -X DELETE "http://localhost:8080/tasks/$id"
done
```

Stop the server with `Ctrl-C` in Terminal A. Expected: `shutdown signal received` → `server stopped`.

---

## 5. Phase 4 — Local Web UI CRUD

With `make run` still active, open **http://localhost:8080/**.

Walk through:
1. Empty state shows “No tasks yet”.
2. Create a task with title + description.
3. Verify it appears with “Open” status and the creation timestamp.
4. Click the round toggle → title gets struck through, pill changes to “Done”.
5. Click **Edit** → change title/description, check “Mark as completed”, save.
6. Return to list → changes visible.
7. Submit a blank title → `400` page with “title is required”.
8. Delete → task disappears; confirm dialog appears (optional JS).
9. Visit `/ui/tasks/999999/edit` → styled “Task not found” page.
10. Visit `/static/style.css` → CSS loads (200).
11. Try a title like `<script>alert(1)</script>` → it renders as text, not script.

Checklist:
- [ ] Create, list, edit, toggle, delete all work
- [ ] Validation errors are visible and preserve context
- [ ] 404/400 error pages render
- [ ] HTML escaping works

---

## 6. Phase 5 — Docker Compose CRUD + persistence

Stop the local server first so port 8080 is free. The local Postgres container
can stay up; Compose does not publish 5432.

```bash
docker compose up -d --build
docker compose ps
```

Compose builds the `production` stage of the Dockerfile. Wait until both
services show `healthy`.

```bash
BASE=http://localhost:8080
curl -sS http://localhost:8080/health

curl -sS -X POST "$BASE/tasks" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Compose task","description":"inside compose"}'
```

Persistence checks:

```bash
# Restart only the app; task must survive
docker compose restart app
sleep 3
curl -sS "$BASE/tasks" | jq

# Restart the whole stack; named volume keeps data
docker compose down
docker compose up -d
sleep 3
curl -sS "$BASE/tasks" | jq
```

Distroless / non-root checks:

```bash
docker image inspect taskmanager:latest --format 'user={{.Config.User}} size={{.Size}}'
docker run --rm --entrypoint /bin/sh taskmanager:latest -c true \
  && echo "shell present (unexpected)" \
  || echo "no shell (distroless confirmed)"
```

Reset Compose data (destructive):

```bash
docker compose down -v
```

Checklist:
- [ ] Compose stack becomes healthy
- [ ] CRUD works through Compose
- [ ] Data survives `restart app` and `down`/`up`
- [ ] Image is distroless and runs as non-root

### 6.1 Building the three stages

The single Dockerfile has three stages, each built explicitly:

```bash
# Compile-only stage (CI artifact) — no ports, env, or healthcheck
make docker-build-stage
# docker build --target build -t taskmanager:build .

# Development stage — Go toolchain + Air
make docker-dev-build
# docker build --target dev -t taskmanager:dev .

# Production stage — distroless, non-root, binary only
make docker-build
# docker build --target production -t taskmanager:latest .
```

> `production` is the final stage, so plain `docker build -t taskmanager:latest .`
> also builds the production image without `--target`.

Check the production image:

```bash
docker image inspect taskmanager:latest --format 'user={{.Config.User}} size={{.Size}}'
docker run --rm --entrypoint /bin/sh taskmanager:latest -c true \
  && echo "shell present (unexpected)" \
  || echo "no shell (distroless confirmed)"
```

Checklist:
- [ ] `build` target produces `/out/taskmanager` and has no runtime config
- [ ] `dev` stage starts Air and serves `localhost:8080`
- [ ] `production` stage is distroless, non-root, and has no Go toolchain
- [ ] Production healthcheck reaches `/ready` via the binary

### 6.2 Development stage (live reload)

The Dockerfile's `dev` stage is the development counterpart to `production`. It
keeps the full Go toolchain and runs [Air](https://github.com/air-verse/air),
which rebuilds and restarts the server when a `.go`, template, or CSS file
changes. Air reads `.air.toml`, which is copied into the image and is also
visible through the source bind mount.

There is no separate Compose development override. Start the Compose Postgres
service, then run the dev image with the source bind-mounted:

```bash
docker compose up -d postgres
docker build --target dev -t taskmanager:dev .

docker run --rm -p 8080:8080 \
  --name taskmanager-dev \
  --network taskmanager_net \
  -e APP_ENV=development \
  -e DATABASE_URL='postgres://postgres:postgres@postgres:5432/tasks?sslmode=disable' \
  -v "$PWD:/src" \
  taskmanager:dev
```

`taskmanager_net` is the network Compose creates for the `backend` network.
Air watches the bind-mounted source; edit a file and watch `building...` /
`running...` in the terminal. `Ctrl-C` stops the container; `docker compose down`
stops Postgres.

Verify the database-aware healthcheck:

```bash
BASE=http://localhost:8080

curl -sS -w ' HTTP %{http_code}\n' "$BASE/health"   # 200 {"status":"ok"}
curl -sS -w ' HTTP %{http_code}\n' "$BASE/ready"    # 200 {"status":"ready"}

# Break the dependency and watch readiness fail while liveness stays 200
docker compose stop postgres
curl -sS -w ' HTTP %{http_code}\n' "$BASE/health"   # still 200
curl -sS -w ' HTTP %{http_code}\n' "$BASE/ready"    # 503 unavailable
docker compose start postgres
sleep 5
curl -sS -w ' HTTP %{http_code}\n' "$BASE/ready"    # 200 again, no restart needed
```

Checklist:
- [ ] Dev image builds and Air starts from `.air.toml`
- [ ] Editing a watched file triggers an Air rebuild (`building...` / `running...`)
- [ ] `/health` stays `200` while Postgres is down; `/ready` returns `503`
- [ ] `/ready` recovers on its own after Postgres returns

> Only one app may bind port 8080 at a time: local `make run`, production
> Compose, or the dev container. The dev container uses the Compose Postgres
> service and its `postgres_data` volume.

---

## 7. Phase 6 — Kubernetes CRUD + persistence

### 7.1 Deploy

```bash
minikube status
kubectl get nodes

# Builds image, loads it into minikube, applies k8s/, waits for rollouts
make k8s-up
```

Inspect:

```bash
kubectl -n taskmanager get deploy,statefulset,pods,pvc,svc
kubectl -n taskmanager get deploy taskmanager -o jsonpath='{.spec.replicas}'
kubectl -n taskmanager logs deploy/taskmanager --tail=20
```

Expected:
- Deployment `1/1`, exactly **1 replica**
- `postgres-0` running, PVC bound (1 Gi)
- no crash loops, probes passing

### 7.2 Access and CRUD

If a port-forward is already running, use **http://localhost:8081**.
Otherwise start one:

```bash
make k8s-forward
```

Run the same CRUD flow with `BASE=http://localhost:8081`, and open the UI at
`http://localhost:8081/`.

### 7.3 Data durability preview

Create a task, then delete the app pod:

```bash
BASE=http://localhost:8081
ID=$(curl -sS -X POST "$BASE/tasks" -H 'Content-Type: application/json' \
  -d '{"title":"Survives restarts"}' | jq -r '.id')

kubectl -n taskmanager delete pod -l app=taskmanager
kubectl -n taskmanager rollout status deploy/taskmanager --timeout=120s

# Restart port-forward if it dropped with the old pod
curl -sS "$BASE/tasks/$ID" | jq
```

Expected: the task still exists (data is in Postgres, not the app pod).

Preview Postgres restart (full chaos testing comes later):

```bash
kubectl -n taskmanager delete pod postgres-0
kubectl -n taskmanager rollout status statefulset/postgres --timeout=180s
curl -sS "$BASE/tasks" | jq
```

Expected: PVC retains the data; the app may return temporary `500`s while the
DB restarts. That is one of the behaviours we will formalise later.

Checklist:
- [ ] App reachable through the Service on 8081
- [ ] API + UI CRUD work in-cluster
- [ ] Task survives app pod deletion
- [ ] Task survives Postgres pod restart (PVC)

### 7.4 Cleanup

```bash
# Remove all Kubernetes resources INCLUDING the namespace and PVC (data loss)
make k8s-down

# Stop the port-forward
kill "$(cat /tmp/opencode/k8s-portforward.pid)" 2>/dev/null || true

# Stop Minikube (keeps cluster state/PVCs)
minikube stop
```

---

## 8. CRUD acceptance matrix

Use this as the pass/fail sheet for each surface (local, Compose, K8s).

| # | Operation | API | UI | Expected |
| --- | --- | --- | --- | --- |
| 1 | Create | `POST /tasks` | Add form | `201`, trimmed title, `Location`, `completed=false` |
| 2 | Read all | `GET /tasks` | `/` | `200`, JSON array / task list |
| 3 | Read one | `GET /tasks/:id` | Edit page | `200`, correct task |
| 4 | Update | `PUT /tasks/:id` | Edit form | `200`, partial fields preserved, `updated_at` changes |
| 5 | Delete | `DELETE /tasks/:id` | Delete button | `204`, then `404` on repeat |
| 6 | Toggle | — | Toggle button | `302/303`, status flips |
| 7 | Validation | invalid body | blank title | `400 validation_error` |
| 8 | Not found | unknown id | unknown edit URL | `404` |
| 9 | Invalid id | `/tasks/abc` | — | `400 invalid_id` |
| 10 | Alias | `/api/v1/tasks` | — | same behaviour |

---

## 9. Next phase — Load & crash-tolerance testing (plan)

> Goal: prove the app survives load, dependency loss, and pod/node restarts
> without data loss, and document the real availability limits of a
> single-replica app + single Postgres instance.

### 9.1 Objectives

- Establish a performance baseline (latency percentiles, error rate).
- Verify graceful shutdown and automatic recovery.
- Verify data durability across app/Postgres/Minikube restarts.
- Identify gaps in probes, restart policies, and deployment strategy.
- Produce repeatable scripts and a short results report.

### 9.2 Tooling to add

```bash
# Lightweight HTTP load generator
GOBIN=$HOME/go/bin go install github.com/rakyll/hey@latest

# Scenario-based load / mixed CRUD
GOBIN=$HOME/go/bin go install github.com/tsenart/vegeta/v12@latest

# Kubernetes metrics (optional, for CPU/memory during load)
minikube addons enable metrics-server
kubectl top pods -n taskmanager
```

### 9.3 Load scenarios

| ID | Scenario | Command sketch | Measure |
| --- | --- | --- | --- |
| L1 | Health endpoint steady load | `hey -z 60s -q 50 http://localhost:8081/health` | p50/p95/p99, non-2xx |
| L2 | Read-heavy list load | `hey -z 60s -q 50 http://localhost:8081/tasks` | latency, DB pool behaviour |
| L3 | Mixed CRUD (80% read / 20% write) | `vegeta`/script against `/tasks` | error rate, `updated_at` correctness |
| L4 | Write churn | create + delete loop | connection leaks, 5xx, row count |

Record for each: requests/sec, p50/p95/p99, success rate, app CPU/memory,
Postgres CPU/memory, and any restarts.

### 9.4 Crash / failure scenarios

| ID | Failure | How to inject | Expected |
| --- | --- | --- | --- |
| C1 | Graceful app shutdown | `kill -TERM <pid>` locally; delete pod in K8s | in-flight requests finish, clean logs, no corruption |
| C2 | App pod kill | `kubectl delete pod -l app=taskmanager` under load | ReplicaSet recreates pod, service recovers, data intact |
| C3 | Rolling update under load | `kubectl rollout restart deploy/taskmanager` | brief interruption with 1 replica (measure it) |
| C4 | Postgres pod kill | `kubectl delete pod postgres-0` under load | app returns temporary 5xx, reconnects, no data loss |
| C5 | DB down at app startup | scale Postgres to 0, restart app, then restore | startup retry + startupProbe prevent crash loop |
| C6 | DB latency / connection exhaustion | optional toxiproxy or `pg_sleep` wrapper | timeouts surface as 5xx; pool recovers |
| C7 | Minikube stop/start | `minikube stop && minikube start` | cluster and PVC data survive |
| C8 | Compose failures | `docker compose stop postgres`, restart app | expected failure mode documented |
| C9 | Resource pressure | stress sidecar or `kubectl top` during load | limits respected, OOM behaviour understood |
| C10 | Bad rollout | deploy an invalid image/tag | rollout blocks, old pod keeps serving, rollback works |

### 9.5 Likely findings / fixes to make before or during testing

- **Readiness checks the DB (done).** `GET /ready` pings PostgreSQL and the
  `readinessProbe` uses it; `/health` remains the liveness probe so a DB outage
  never triggers a restart. Verified locally and on Minikube: with Postgres
  scaled to zero the pod became `Ready=False`, Service endpoints emptied,
  `/health` stayed `200`, `/ready` returned `503`, and the app restarted zero
  times. It returned to `Ready` automatically when Postgres came back.
- **Single replica + `maxUnavailable: 1`** means a rolling update creates a
  short outage. Consider `maxUnavailable: 0, maxSurge: 1` for zero-downtime
  rollouts while still keeping 1 steady replica.
- **No Compose restart policy.** Add `restart: unless-stopped` to app and
  Postgres if daemon restarts must be tolerated.
- **Single Postgres is a SPOF** and has no backups. Document this, and consider
  a `pg_dump` CronJob or a managed database for anything beyond the demo.
- **No PodDisruptionBudget / anti-affinity / NetworkPolicy.** Relevant for
  multi-node clusters, not Minikube.
- **Probe timings** may need tuning once real recovery times are measured.

### 9.6 Acceptance criteria for the next phase

- Baseline load: **0 non-2xx** responses; record p95/p99 targets.
- App pod deletion: service recovers automatically; **zero data loss**.
- Postgres restart: data intact; app reconnects without manual action.
- App startup with DB down: no crash loop; becomes Ready after DB returns.
- Graceful shutdown: no truncated writes; clean shutdown log.
- Every failure scenario has a documented observed RTO and behaviour.
- Results and commands committed as `scripts/` + a short results report.

### 9.7 Deliverables for the next phase

- `scripts/smoke.sh` — reusable CRUD smoke test (local/Compose/K8s via `BASE`).
- `scripts/load.sh` or `scripts/load.js` — repeatable load scenarios.
- `scripts/chaos.sh` — ordered failure-injection checks with pass/fail output.
- `RESULTS.md` — environment, commands, metrics, observed failures, fixes.
- README section summarising how to run all of it.

---

## 10. Cleanup & reset cookbook

| Goal | Command |
| --- | --- |
| Stop local app | `Ctrl-C` in `make run` terminal |
| Reset local DB rows | `docker exec taskmanager-postgres psql -U postgres -d tasks -c 'TRUNCATE tasks RESTART IDENTITY;'` |
| Stop local DB container | `docker stop taskmanager-postgres` |
| Restart local DB | `docker run -d --rm --name taskmanager-postgres -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=tasks -p 5432:5432 postgres:16-alpine` |
| Reset Compose + data | `docker compose down -v` |
| Stop Compose | `docker compose down` |
| Start dev container | see section 6.2 (`docker run ... taskmanager:dev`) |
| Stop dev container | `Ctrl-C` or `docker stop taskmanager-dev` |
| Delete K8s resources + PVC | `make k8s-down` |
| Stop port-forward | `kill "$(cat /tmp/opencode/k8s-portforward.pid)"` |
| Restart port-forward | `make k8s-forward` |
| Stop Minikube | `minikube stop` |

Destructive warnings:
- `docker compose down -v` deletes the Compose Postgres volume.
- `make k8s-down` deletes the `taskmanager` namespace and its PVC.
- `minikube delete` deletes the whole cluster and all PVC data.

---

## 11. Troubleshooting

| Symptom | Check / fix |
| --- | --- |
| `bind: address already in use` on 8080 | stop the other stack (`Ctrl-C`, `docker compose down`, or `ss -ltnp \| grep 8080`) |
| `go: command not found` | use `/usr/local/go/bin/go` or `export PATH=$PATH:/usr/local/go/bin` |
| Integration test skipped | set `TEST_DATABASE_URL` |
| DB connection refused locally | `docker ps` for `taskmanager-postgres`; restart it (see cookbook) |
| Minikube API server unhealthy | avoid concurrent `minikube start`; run `minikube stop` then a single `minikube start` |
| K8s pod `ImagePullBackOff` | run `minikube image load taskmanager:latest`; keep `imagePullPolicy: IfNotPresent` |
| Port-forward dies after pod restart | restart `make k8s-forward` (it binds to a pod, not a stable proxy) |
| Pod stuck before Ready | `kubectl -n taskmanager describe pod <pod>` and `kubectl -n taskmanager logs <pod>`; startupProbe allows 120s |
| Pod `Ready=False`, app running | check `/ready` and `kubectl -n taskmanager get endpoints taskmanager`; readiness fails when the DB is unreachable (expected during DB outages) |
| Dev container does not rebuild on save | confirm `.air.toml` has `poll = true` (Docker Desktop bind mounts may not emit inotify events) and that `.:/src` is mounted |
| PVC Pending | `kubectl get storageclass`; Minikube should provide `standard` |

---

## 12. Suggested order

1. Phase 1–2: code tour + automated checks.
2. Phase 3–4: local API and UI CRUD.
3. Phase 5: Compose CRUD + persistence.
4. Phase 6: Kubernetes CRUD + persistence.
5. Phase 7: fill in the acceptance matrix.
6. Phase 9: add `/ready`, restart policies, and test scripts.
7. Run load scenarios, then failure scenarios; record results.
