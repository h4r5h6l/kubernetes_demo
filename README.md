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
- **Static binary:** `CGO_ENABLED=0`, `GOOS=linux`, `GOARCH=amd64`, built with
  `-trimpath -ldflags="-s -w"`, required by `distroless/static`.
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
make docker-build          # docker build --target production -t taskmanager:latest .
make docker-build-stage    # compile-only artifact
make docker-dev-build      # development image with Air
docker build .             # production, because it is the final stage
```

The production image is approximately **38.5 MB**, runs as non-root, and has a
`HEALTHCHECK` that calls the binary against `/ready`.

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
PVC, Deployment with one replica, Service, optional Ingress). See
`WALKTHROUGH.md` for the full walkthrough and verification steps.

## Documentation

- `PLAN.md` — architecture, milestones, and design decisions
- `WALKTHROUGH.md` — code tour and step-by-step verification
