# syntax=docker/dockerfile:1

# ---- build stage -------------------------------------------------------------
FROM golang:1.24-alpine AS build

ARG VERSION=dev
WORKDIR /src

# Dependencies first so the layer is cached across source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go vet ./... \
 && CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/api ./cmd/api

# A passwd entry for the unprivileged user the final stage runs as.
RUN printf 'athletica:x:10001:10001::/nonexistent:/sbin/nologin\n' > /out/passwd

# ---- runtime stage -----------------------------------------------------------
# scratch: the image contains the static binary and nothing else — no shell,
# no package manager, nothing to exploit. Docker's health check calls the
# binary's own `healthcheck` subcommand instead of curl/wget.
FROM scratch AS runtime

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/passwd /etc/passwd
COPY --from=build /out/api /api

USER 10001:10001
EXPOSE 8080

ENV ATHLETICA_HTTP_ADDR=":8080" \
    ATHLETICA_BASE_PATH="/api/v1"

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
    CMD ["/api", "healthcheck"]

ENTRYPOINT ["/api"]
CMD ["serve"]
