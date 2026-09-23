FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o orchestrator ./cmd/orchestrator

FROM alpine:3.20
RUN adduser -D -h /app appuser
WORKDIR /app
COPY --from=builder /app/orchestrator .
COPY --from=builder /app/migrations ./migrations
USER appuser

EXPOSE 8080
CMD ["./orchestrator"]
