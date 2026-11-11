# Task Manager — Go + Gin + PostgreSQL

A small CRUD task manager with a server-rendered web UI and a JSON API. It is
packaged as a static Go binary in a distroless, non-root container and can run
with Docker Compose or Kubernetes.

## Stack

| Layer | Choice |
| --- | --- |
| Language | Go 1.23 (`go 1.23.0` in `go.mod`) |
| HTTP | `net/http` server + Gin v1.11.0 router |
| Database | PostgreSQL 16 (`postgres:16-alpine`) |
| Driver | `pgx/v5` (`pgxpool`) |
| Migrations | Embedded SQL, applied at startup |
| Build image | `golang:1.23-alpine` |
| Runtime image | `gcr.io/distroless/static-debian12:nonroot` |

## Quick start

```bash
docker compose up -d --build
docker compose ps
```

Then open <http://localhost:8080>.

```bash
curl -sS http://localhost:8080/health   # {"status":"ok"}
curl -sS http://localhost:8080/ready    # {"status":"ready"}
```

Stop the stack, keeping the database volume:

```bash
docker compose down
```

## API

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/tasks` | Create a task (`201` + `Location`) |
| `GET` | `/tasks` | List tasks |
| `GET` | `/tasks/{id}` | Fetch one task |
| `PUT` | `/tasks/{id}` | Update title, description, or completion |
| `DELETE` | `/tasks/{id}` | Delete a task (`204`) |

Every route is also mounted under `/api/v1`, for example
`POST /api/v1/tasks` and `DELETE /api/v1/tasks/1`.

Example:

```bash
curl -sS -X POST http://localhost:8080/tasks \
  -H 'Content-Type: application/json' \
  -d '{"title":"Write the report","description":"Include assumptions"}'

curl -sS http://localhost:8080/tasks
curl -sS -X PUT http://localhost:8080/tasks/1 \
  -H 'Content-Type: application/json' -d '{"completed":true}'
curl -sS -X DELETE http://localhost:8080/tasks/1 -i   # 204 No Content
```

The web UI is served at `/` and uses form posts under `/ui/tasks`. Deletion is a
two-step, JavaScript-free flow: `GET /ui/tasks/{id}/delete` renders a
confirmation page and `POST` to the same path performs the delete.

## Configuration

| Variable | Default | Used by | Meaning |
| --- | --- | --- | --- |
| `APP_ENV` | `development` | app | `production` disables `.env` loading |
| `PORT` | `8080` | app | HTTP listen port (validated 1–65535) |
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/tasks?sslmode=disable` | app | PostgreSQL DSN |
| `POSTGRES_USER` | `postgres` | Compose | Database user |
| `POSTGRES_PASSWORD` | `postgres` | Compose | Database password |
| `POSTGRES_DB` | `tasks` | Compose | Database name |
| `TEST_DATABASE_URL` | unset | tests | Optional repository integration tests |

Copy `.env.example` to `.env` for local development. `.env` is ignored by Git.

## Assumptions

- **Go version:** the module targets Go 1.23 and the image is built with
  `golang:1.23-alpine`. Code must remain compatible with the Go 1.23 language
  and standard library.
- **Server:** the application runs the standard `net/http` server with timeouts
  and graceful shutdown; Gin is only the router/handler layer.
- **Ports:** the app listens on `:8080` inside the container and is published as
  `8080:8080`. PostgreSQL listens on `5432` but is **not** published; it is only
  reachable on the internal Compose network as `postgres:5432`.
- **Database:** PostgreSQL 16 is the only supported database. `sslmode=disable`
  is used inside the private Compose network; a managed database should enable
  TLS via `DATABASE_URL`.
- **Credentials:** Compose reads `POSTGRES_USER`, `POSTGRES_PASSWORD`, and
  `POSTGRES_DB` from the environment with development defaults. Real deployments
  should inject `DATABASE_URL` as a secret.
- **Migrations:** SQL files are embedded in the binary (`migrations/`), applied
  at startup in a transaction, tracked in `schema_migrations`, and guarded by a
  PostgreSQL advisory lock. No external migration tool is required; migrations
  must be safe to re-run.
- **Startup order:** the app waits for PostgreSQL to pass `pg_isready`
  (`depends_on: condition: service_healthy`) and retries the initial connection.
- **Persistence:** all database data lives in the named volume `postgres_data`.
- **Runtime user:** the production container runs as `nonroot:nonroot`
  (UID/GID 65532) from distroless; it contains no shell, package manager, or Go
  toolchain.
- **Static binary:** `CGO_ENABLED=0` is required by `distroless/static`, which
  has no libc; a dynamically linked binary would fail to start.
- **Health:** `/health` is liveness only (no database access); `/ready` pings
  PostgreSQL and returns `503` when it is unavailable.

## Containers

The multi-stage `Dockerfile` has three targets:

| Target | Base | Purpose |
| --- | --- | --- |
| `build` | `golang:1.23-alpine` | Compile the static binary at `/out/taskmanager` |
| `dev` | `golang:1.23-alpine` | Go toolchain + Air live reload (`.air.toml`) |
| `production` | `gcr.io/distroless/static-debian12:nonroot` | Final/default stage, binary only |

```bash
make docker-build          # docker build -t taskmanager:latest .
make docker-dev-build      # development image with Air
docker build .             # production, because it is the final stage
```

The production image is approximately **38.5 MB**, runs as non-root, and has a
`HEALTHCHECK` that calls the binary against `/ready`.

### Dockerfile at a glance

| Instruction | Purpose |
| --- | --- |
| `FROM golang:1.23-alpine AS build` | Official Go image with the full toolchain |
| `WORKDIR /src` | Build directory inside the image |
| `COPY go.mod go.sum` + `RUN go mod download` | Cache dependencies in their own layer |
| `COPY . .` | Copy the source into the build stage |
| `RUN CGO_ENABLED=0 go build ...` | Static binary, required by distroless |
| `FROM .../static-debian12:nonroot` | Distroless runtime: no shell, non-root |
| `COPY --from=build` | Copy only the compiled binary |
| `EXPOSE 8080` | Document the listen port |
| `USER nonroot:nonroot` | Run the process unprivileged |
| `HEALTHCHECK ...` | Call the binary against `/ready` |
| `ENTRYPOINT` | Start the server |

## Compose services

`docker-compose.yaml` defines:

- **network:** `backend`, explicitly named `taskmanager_net` (bridge driver)
- **volume:** `postgres_data` mounted at `/var/lib/postgresql/data`
- **services:** `app` (built from the `production` target) and `postgres`
- **restart policy:** `unless-stopped` for both services
- **startup:** `app` waits for the Postgres healthcheck
- **ports:** only `8080` is published

## Local development

```bash
make run      # uses DATABASE_URL from .env or the localhost default
make test     # unit tests
make vet
```

Run the dev container manually with a bind-mounted source tree:

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

Air watches the bind-mounted source and rebuilds on change.

## Kubernetes

Manifests live in `k8s/` (namespace, secret, configmap, Postgres StatefulSet +
PVC, Deployment with one replica, Service, Ingress, and an HPA for load
testing). See `WALKTHROUGH.md` for the full walkthrough and verification steps.

### Health probes

All probes are real HTTP `GET` requests against the application's own endpoints
on the named container port `http` (8080). No generic TCP socket probes are used.

| Probe | Path | Checks | Why |
| --- | --- | --- | --- |
| `startupProbe` | `/health` | process is serving HTTP | Gives the app up to 120 s to connect and migrate without liveness restarting it |
| `livenessProbe` | `/health` | process is serving HTTP | Restarts a wedged process; deliberately does **not** touch PostgreSQL, so a DB outage cannot cause a restart loop |
| `readinessProbe` | `/ready` | process + `SELECT 1` against PostgreSQL | Removes the pod from Service endpoints while the database is unavailable |

The assignment asks for `/health` probes. `/health` is used for startup and
liveness. Readiness intentionally uses `/ready` because it is the application's
DB-aware readiness endpoint: it returns `200 {"status":"ready"}` only when the
connection pool can reach PostgreSQL, and `503 {"status":"unavailable"}`
otherwise. Pointing readiness at `/health` would keep sending traffic to a pod
that cannot serve CRUD requests during a database outage. This was verified on
Minikube: with PostgreSQL scaled to zero the pod became `Ready=False`, Service
endpoints emptied, `/health` stayed `200`, `/ready` returned `503`, and the app
restarted zero times.

### Service exposure

`service.yaml` uses `type: ClusterIP` deliberately:

- The app is an internal backend. Its callers are the Ingress controller and
  in-cluster clients, not external clients directly.
- ClusterIP gives a stable virtual IP and DNS name (`taskmanager.taskmanager.svc`)
  independent of pod churn and HPA replica changes.
- Local access uses `kubectl -n taskmanager port-forward svc/taskmanager 8081:80`.
  Browser/host access uses the Ingress (`taskmanager.local`).
- `NodePort` would expose an implementation-specific port on every node, and
  `LoadBalancer` would request an external cloud load balancer. Neither is
  needed on Minikube, and both add exposure and cost without adding routing
  capability already provided by the Ingress.

### Autoscaling and load test

`k8s/hpa.yaml` is an `autoscaling/v2` HPA (CPU target 70% of the `200m` request,
1–5 replicas). It is included in `k8s/kustomization.yaml` for load testing. The
Deployment manifest still declares exactly `spec.replicas: 1`; while the HPA
exists it owns the live replica count.

The accepted 500-concurrent-user run (500 VUs, 2 minutes, through the Ingress)
completed with **0 failed requests**, **100% checks**, **504.6 req/s**, and a
**p95 of 518 ms**. The HPA scaled from 1 to 3 ready pods during the run. Full
methodology, commands, HPA trace, and the discarded baseline are in
`loadtest/README.md`.

## Documentation

- `PLAN.md` — architecture, milestones, and design decisions
- `WALKTHROUGH.md` — code tour and step-by-step verification
- `loadtest/README.md` — load-test profiles, commands, results, and HPA evidence
