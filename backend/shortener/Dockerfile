# --- Build stage ---
FROM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /api ./cmd/shortener

# --- Runtime stage ---
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 appuser

WORKDIR /app

COPY --from=builder /api .
COPY data/ ./data/
COPY migrations/ ./migrations/

USER appuser

EXPOSE 8080 9091

CMD ["./api"]
