# Build Stage
FROM golang:1.25-alpine AS builder
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /app

# Copy workspace config and module dependencies
COPY go.work ./
COPY gateway/go.mod gateway/go.sum ./gateway/
COPY ledger/go.mod ledger/go.sum ./ledger/

# Sync workspace dependencies
RUN go work sync

# Copy source code
COPY gateway/ ./gateway/
COPY ledger/ ./ledger/

# Build gateway
RUN CGO_ENABLED=0 GOOS=linux go build -o gateway ./gateway/cmd/gateway/main.go

# Production Stage
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/gateway .
EXPOSE 8080
CMD ["./gateway"]
