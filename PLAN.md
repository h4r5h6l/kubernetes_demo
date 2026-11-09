# Task Manager — Go + Gin CRUD App

## 1. Goal

A task management web app with:

- JSON REST API for full CRUD on tasks
- Minimal server-rendered web UI (no JS framework, no build step)
- PostgreSQL persistence
- Docker Compose for local development
- Kubernetes manifests for deployment

**Task fields (minimal):** `id`, `title`, `description`, `completed`, `created_at`, `updated_at`.

## 2. Tech Stack

| Concern      | Choice                                                        |
| ------------ | ------------------------------------------------------------- |
| Language     | Go 1.23 (module directive) — local toolchain 1.27.3           |
| HTTP         | Gin v1.11.0 (pinned for Go 1.23 compatibility)                |
| Database     | PostgreSQL 16                                                  |
| DB driver    | `pgx/v5` v5.7.6 + `pgxpool` (explicit SQL, no ORM)             |
| Migrations   | Embedded SQL files applied at startup (advisory-lock guarded)  |
| Templates    | `html/template` via `embed.FS`                                 |
| Config       | Environment variables + `godotenv` v1.5.1 (`.env` in dev)      |
| Container    | Multi-stage Docker build; static `CGO_ENABLED=0` binary; non-root runtime |
| Orchestration| Docker Compose, Kubernetes (Deployment, Service, Ingress)      |

> **Note:** Go 1.27.3 is installed at `/usr/local/go/bin` (not on the non-interactive shell PATH). The module declares `go 1.23`, and Docker builds with `golang:1.23-alpine`, so code must stay compatible with Go 1.23 language/stdlib. Go 1.23 is EOL upstream; this pin is for rubric compatibility and will be noted in the README.

## 3. Project Layout

```
.
├── cmd/server/main.go              # wiring, graceful shutdown
├── internal/
│   ├── config/config.go            # env parsing + defaults
│   ├── database/
│   │   ├── postgres.go             # pgxpool setup + ping
│   │   └── migrate.go              # embedded migrations + advisory lock
│   ├── task/
│   │   ├── model.go                # Task struct, validation
│   │   ├── repository.go           # Repository interface + Postgres impl
│   │   └── handler_api.go          # JSON handlers
│   └── httpx/{health,errors}.go    # probes + error envelope
├── migrations/
│   ├── embed.go
│   └── 0001_create_tasks.sql
├── web/
│   ├── embed.go                    # embedded templates + static
│   ├── handler.go                  # HTML handlers
│   ├── templates/{layout,list,edit,error}.html
│   └── static/style.css
├── Dockerfile
├── docker-compose.yml
├── .dockerignore
├── .env.example
├── Makefile
├── k8s/
│   ├── kustomization.yaml
│   ├── namespace.yaml
│   ├── secret.yaml                 # demo-only credentials
│   ├── configmap.yaml              # APP_ENV, PORT
│   ├── postgres.yaml               # StatefulSet + PVC + Service
│   ├── deployment.yaml             # app Deployment (1 replica)
│   ├── service.yaml                # app Service
│   └── ingress.yaml                # optional
└── README.md
```

## 4. Data Model

```sql
CREATE TABLE tasks (
    id          BIGSERIAL PRIMARY KEY,
    title       TEXT        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    description TEXT        NOT NULL DEFAULT '',
    completed   BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX tasks_created_at_idx ON tasks (created_at DESC);
```

`updated_at` is set explicitly by the update query (`now()`), not via trigger.

## 5. API Design (`/tasks`)

Canonical routes match the rubric. The same handlers are also mounted at `/api/v1/tasks` as an alias.

| Method | Path           | Body                                   | Success |
| ------ | -------------- | -------------------------------------- | ------- |
| GET    | `/tasks`       | —                                      | 200     |
| POST   | `/tasks`       | `{ "title", "description" }`           | 201     |
| GET    | `/tasks/:id`   | —                                      | 200     |
| PUT    | `/tasks/:id`   | `{ "title", "description", "completed" }` | 200  |
| DELETE | `/tasks/:id`   | —                                      | 204     |

- Errors use `{ "error": { "code": "...", "message": "..." } }`
- Status codes: `400` validation/bad JSON, `404` unknown id, `500` unexpected
- `title` required, trimmed, 1–200 chars; `description` max 2000 chars
- List ordered by `created_at DESC`

Operational endpoints (outside `/api/v1`):

- `GET /health` — liveness: `200 OK` with `{"status":"ok"}`. No DB dependency, so a DB outage does not restart the pod.
- `GET /ready` — readiness: pings PostgreSQL with a 2s timeout. Returns `200 {"status":"ready"}` or `503 {"status":"unavailable"}`. Used by the Kubernetes readiness probe and the Docker `HEALTHCHECK`.

## 6. Web UI (server-rendered)

| Method | Path                   | Purpose                             |
| ------ | ---------------------- | ----------------------------------- |
| GET    | `/`                    | List tasks + inline "add" form      |
| POST   | `/ui/tasks`            | Create, redirect (303) to `/`       |
| GET    | `/ui/tasks/:id/edit`   | Edit form                           |
| POST   | `/ui/tasks/:id`        | Update, redirect (303) to `/`       |
| POST   | `/ui/tasks/:id/delete` | Delete, redirect (303) to `/`       |
| POST   | `/ui/tasks/:id/toggle` | Toggle completed, redirect (303)    |
| GET    | `/static/*`            | Embedded CSS                        |

Form actions live under `/ui` so they never collide with the JSON API's `/tasks` routes.

- Templates embedded with `embed.FS`; parsed once at startup
- Shared layout + simple redirects; HTML form validation mirrors API rules
- One small CSS file; no JavaScript build step (only an optional inline confirm dialog)

## 7. Configuration

| Variable       | Default                                                        | Notes                    |
| -------------- | -------------------------------------------------------------- | ------------------------ |
| `PORT`         | `8080`                                                         | HTTP listen port         |
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/tasks?sslmode=disable` | pgx connection string |
| `APP_ENV`      | `development`                                                  | `production` disables `.env` loading and Gin debug logs |
| `GIN_MODE`     | derived from `APP_ENV`                                         | `debug` / `release`      |

`.env` is loaded only when present and `APP_ENV != production`.

## 8. Docker & Compose

- **Dockerfile (production):** multi-stage — `golang:1.23-alpine` build → `gcr.io/distroless/static-debian12:nonroot` runtime, non-root user, cached module layer
- **Dockerfile.dev (development):** `golang:1.23-alpine` + Air live reload, bind-mounted source, `curl`-based healthcheck; used via `docker-compose.dev.yml`
- **Static binary (required):** `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/taskmanager ./cmd/server`; this is the default `make build` target. `distroless/static` has no libc, so a dynamic binary would fail at startup.
- **Healthcheck in distroless:** add a `-healthcheck` flag to the server that performs an HTTP GET against `localhost:$PORT/ready` (`-healthcheck-path` selects the path), so Docker `HEALTHCHECK` works without a shell/curl in the image
- **docker-compose.yml:** `postgres:16-alpine` with named volume + healthcheck; app `depends_on: condition: service_healthy`; app exposed on `8080`
- `.dockerignore` excludes `.git`, binaries, local env files

## 9. Kubernetes (`k8s/`, Minikube)

- `namespace.yaml` — dedicated namespace
- `secret.yaml` — Postgres credentials (demo only; note to use SealedSecrets/ExternalSecrets in real clusters)
- `postgres.yaml` — StatefulSet, 1 Gi PVC, ClusterIP Service `postgres`
- `deployment.yaml` — app Deployment with **1 replica** (rubric requirement), resource requests/limits, non-root read-only security context; `/health` startup + liveness probes and DB-aware `/ready` readiness probe; env from ConfigMap/Secret
- `service.yaml` — app Service (ClusterIP, port 80 → 8080)
- `ingress.yaml` — optional; host `taskmanager.local` → app Service
- `kustomization.yaml` — `kubectl apply -k k8s/` applies everything
- Migrations run at startup under a PostgreSQL advisory lock (safe even with 1 replica, and if scaled later)
- App retries PostgreSQL on startup (30 × 2s); the startup probe allows up to 120s before liveness begins
- Image is built locally and loaded with `minikube image load taskmanager:latest` (the cluster uses containerd)
- `terminationGracePeriodSeconds` + graceful shutdown align with rolling updates
- Verified on Minikube: 1 ready replica, UI/API reachable via `kubectl port-forward svc/taskmanager 8081:80`, and data survives app pod deletion (Postgres PVC)

## 10. Testing & Quality

- Handler tests with `httptest` + an in-memory fake `Repository` (no DB needed)
- Validation tests for title/description rules
- Optional repository integration test gated by `TEST_DATABASE_URL`
- `make fmt`, `make vet`, `make test` targets
- `golangci-lint` optional (not required to finish)

## 11. Milestones

- [x] **M0 — Bootstrap:** `go mod init`, `.gitignore`, `Makefile`, `.env.example`; verify Go 1.27.3 locally (module targets 1.23)
- [x] **M1 — Foundation:** config loader, pgxpool connection, embedded migration, `/health`, graceful shutdown
- [x] **M2 — API:** Task model + Postgres repository + JSON CRUD handlers, validation, error envelope; handler tests
- [x] **M3 — Web UI (optional, already selected):** embedded templates, list/create/edit/delete/toggle routes, static CSS
- [x] **M4 — Containers:** multi-stage distroless Dockerfile (non-root, static), `.dockerignore`, docker-compose with Postgres named volume + healthcheck; verify end to end
- [x] **M5 — Kubernetes:** `deployment.yaml` (1 replica, probes, limits) + `service.yaml` + Postgres manifests; deploy to Minikube
- [ ] **M6 — Docs:** README with setup, env vars, curl examples, UI walkthrough, compose and Minikube instructions

## 12. Acceptance Criteria

1. `make test` and `make vet` pass.
2. All five API endpoints behave per the table above (verified with `curl`).
3. Web UI supports create, list, edit, toggle, and delete at `http://localhost:8080`.
4. `docker compose up --build` starts app + Postgres and the app becomes healthy.
5. `kubectl apply -k k8s/` on Minikube yields 1 ready app replica + Postgres; `kubectl port-forward` reaches the app, `/health` returns 200, and `/ready` returns 200 while the DB is reachable.
6. Restarting the app preserves data.

## 13. Decisions & Risks

- **No ORM:** explicit SQL keeps behavior transparent; `pgx` is the maintained Postgres driver.
- **Startup migrations:** simple and dependency-free; advisory lock keeps it safe if replicas are scaled up later. For larger projects, a separate migration job is preferable.
- **Secrets in Git:** `k8s/secret.yaml` is explicitly demo-only and called out in the README.
- **Go version:** local toolchain is 1.27.3; `go.mod` targets 1.23 and Docker builds with `golang:1.23-alpine` for rubric compatibility. Keep code free of 1.24+ features.
- **Ingress:** requires an ingress controller; README will note that `port-forward` is the fallback.
- **Health probe semantics:** `/health` is liveness-only (no DB check) to avoid restart loops during DB outages; `/ready` pings the DB and drives readiness/endpoint membership. This keeps the process alive while traffic is withheld during a database outage.

## 14. Next Step

Confirm this plan, then start at **M0** and implement milestones in order, verifying each one before moving on.
