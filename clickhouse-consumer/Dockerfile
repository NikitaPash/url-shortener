FROM golang:1.26-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /consumer ./cmd/consumer

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 appuser
WORKDIR /app
COPY --from=builder /consumer .
USER appuser

EXPOSE 9094

CMD ["./consumer"]
