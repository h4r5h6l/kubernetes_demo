# Task Manager

A Go task manager with a JSON API, server-rendered web UI, and PostgreSQL. It
runs with Docker Compose or Kubernetes (Minikube).

## Repository structure

```text
.
├── cmd/server/       # Application entry point
├── internal/         # Configuration, database, API, and health handlers
├── migrations/       # Embedded PostgreSQL schema migrations
├── web/              # HTML handlers, templates, and static assets
├── k8s/              # Kubernetes app, database, service, ingress, and HPA
├── loadtest/         # k6 script, results, and load-test report
├── Dockerfile
├── docker-compose.yaml
├── Makefile
└── README.md
```

## Run locally

```bash
docker compose up -d --build
```

Open <http://localhost:8080>. Check the app and database connection with
`curl http://localhost:8080/ready`. Stop the stack with `docker compose down`.

## API

All routes are also available under `/api/v1`.

| Method | Path | Success response |
| --- | --- | --- |
| `POST` | `/tasks` | `201 Created` |
| `GET` | `/tasks` | `200 OK` |
| `GET` | `/tasks/{id}` | `200 OK` |
| `PUT` | `/tasks/{id}` | `200 OK` |
| `DELETE` | `/tasks/{id}` | `204 No Content` |

The web interface is served at `/`. Liveness is checked at `/health`; database
readiness is checked at `/ready`.

## Kubernetes

Deploy the manifests to Minikube with `make k8s-up`. The deployment starts with
one replica; the included HPA scales it from 1 to 5 based on a 70% CPU target.
Enable metrics first with `minikube addons enable metrics-server`. The HPA
percentage is CPU usage relative to the pod's 200m CPU request, so 70% equals
140m per pod. It is a scaling target, not a CPU cap.

For local access, run `make k8s-forward`, then use <http://localhost:8081>.

## Validation results

The committed k6 acceptance run used 500 virtual users for 2 minutes through
the NGINX Ingress on Minikube.

| Measure | Result |
| --- | ---: |
| Requests | 61,063 |
| Throughput | 504.6 requests/s |
| Failed requests / checks | 0 / 0 (0%) |
| p90 / p95 latency | 299.5 ms / 518.1 ms |
| Maximum latency | 1.68 s |
| HPA replicas | Scaled from 1 to 3 |

CPU was the scaling signal under this load; the available run evidence does not
include memory samples, so memory pressure cannot be ruled out independently.
The HPA trace, k6 output, and reproducible commands are in
[`loadtest/README.md`](loadtest/README.md), [`loadtest/load500-2m.txt`](loadtest/load500-2m.txt),
and [`loadtest/hpa-during-load500.txt`](loadtest/hpa-during-load500.txt).

The Kubernetes probes use `/health` for liveness and `/ready` for database
readiness. The committed evidence covers HPA scale-out; capture before/after
`kubectl get pods` output during a container restart to document the restart
count requirement.

## Checks

```bash
make test
make vet
```
