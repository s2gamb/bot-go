# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sync-groups ./cmd/sync-groups

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 10001 -S botuser && \
    adduser -u 10001 -S -G botuser botuser && \
    mkdir -p /app/data && \
    chown -R botuser:botuser /app

COPY --from=builder /out/bot /usr/local/bin/bot
COPY --from=builder /out/sync-groups /usr/local/bin/sync-groups

USER botuser
WORKDIR /app

ENTRYPOINT ["/usr/local/bin/bot"]
