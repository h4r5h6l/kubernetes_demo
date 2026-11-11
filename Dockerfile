# 1) Build: official Go image with the full toolchain.
FROM golang:1.23-alpine AS build

WORKDIR /src

# Download modules first so this layer is cached while source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO is off so the binary is static and runs on distroless (no libc).
RUN CGO_ENABLED=0 go build -o /out/taskmanager ./cmd/server

# -----------------------------------------------------------------------------
# 2) Dev: full toolchain plus Air live reload. Source is bind-mounted.
# -----------------------------------------------------------------------------
FROM golang:1.23-alpine AS dev

RUN apk add --no-cache git \
    && go install github.com/air-verse/air@v1.61.7

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV APP_ENV=development \
    PORT=8080

EXPOSE 8080

CMD ["air", "-c", ".air.toml"]

# -----------------------------------------------------------------------------
# 3) Production: default target. Distroless, non-root, binary only.
# Must remain the final stage so `docker build .` builds production.
# -----------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS production

WORKDIR /app

COPY --from=build /out/taskmanager /app/taskmanager

EXPOSE 8080

USER nonroot:nonroot

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD ["/app/taskmanager", "-healthcheck", "-healthcheck-path", "/ready"]

ENTRYPOINT ["/app/taskmanager"]
