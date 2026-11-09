# syntax=docker/dockerfile:1

# -----------------------------------------------------------------------------
# build: compile the static Linux binary at /out/taskmanager.
# Used by the production stage and available as a CI artifact target.
# -----------------------------------------------------------------------------
FROM golang:1.23-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -buildvcs=false -trimpath -ldflags="-s -w" \
    -o /out/taskmanager ./cmd/server

# -----------------------------------------------------------------------------
# dev: full toolchain plus Air live reload. Source is bind-mounted by Compose.
# -----------------------------------------------------------------------------
FROM golang:1.23-alpine AS dev

ARG AIR_VERSION=v1.61.7

RUN apk add --no-cache curl git \
    && go install github.com/air-verse/air@${AIR_VERSION}

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ENV APP_ENV=development \
    PORT=8080 \
    CGO_ENABLED=0

EXPOSE 8080

# Readiness includes a database ping; -f makes curl exit non-zero on 503.
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=5 \
    CMD ["curl", "-fsS", "http://127.0.0.1:8080/ready"]

CMD ["air", "-c", ".air.toml"]

# -----------------------------------------------------------------------------
# production: default target. Distroless, non-root, binary only.
# Must remain the final stage so `docker build .` builds production.
# -----------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS production

WORKDIR /app

COPY --from=build /out/taskmanager /app/taskmanager

# Runtime defaults only. DATABASE_URL stays a deployment-time secret.
ENV APP_ENV=production \
    PORT=8080

EXPOSE 8080

USER nonroot:nonroot

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD ["/app/taskmanager", "-healthcheck", "-healthcheck-path", "/ready"]

ENTRYPOINT ["/app/taskmanager"]
