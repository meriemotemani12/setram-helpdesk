# syntax=docker/dockerfile:1

# ---------- Compilation ----------
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY . .
# -mod=mod : complète go.sum au premier build si le fichier n'a pas encore été versionné
# (lancez « go mod tidy » une fois et versionnez go.sum pour des builds reproductibles).
ENV CGO_ENABLED=0 GOFLAGS=-mod=mod
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---------- Exécution ----------
FROM alpine:3.20
# L'interface web et les migrations sont embarquées dans le binaire : un seul fichier à déployer.
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 app
COPY --from=build /out/server /usr/local/bin/server
USER app
ENV ADDR=:8080
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/server"]
