# syntax=docker/dockerfile:1.6

############################
# Stage 1: build
############################
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Install templ generator first (pinned to module version)
RUN go install github.com/a-h/templ/cmd/templ@v0.2.778

# Copy source
COPY . .

# Generate templ Go files BEFORE resolving dependencies so the
# generated package is visible to `go mod tidy`.
RUN templ generate

# Resolve and download dependencies based on the full source tree
RUN go mod tidy

# Build a static binary
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

############################
# Stage 2: runtime
############################
FROM alpine:3.20 AS runtime

RUN apk add --no-cache ca-certificates tzdata wget && \
    addgroup -S harbor && adduser -S harbor -G harbor

WORKDIR /app

COPY --from=builder /out/server /app/server
COPY --from=builder /src/migrations /app/migrations
COPY --from=builder /src/seed /app/seed

# Create writable mount points so docker volumes inherit harbor ownership.
RUN mkdir -p /app/keys /backups /exchange && \
    chown -R harbor:harbor /app /backups /exchange

USER harbor

EXPOSE 8080

ENTRYPOINT ["/app/server"]
