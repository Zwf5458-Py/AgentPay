FROM golang:1.25-alpine
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /app

# Copy workspace config and module dependencies
COPY go.work go.work.sum* ./
COPY gateway/go.mod gateway/go.sum ./gateway/
COPY ledger/go.mod ledger/go.sum ./ledger/
COPY pricing/go.mod pricing/go.sum ./pricing/

# Sync workspace dependencies
RUN go work sync

# Copy source code
COPY gateway/ ./gateway/
COPY ledger/ ./ledger/
COPY pricing/ ./pricing/

# Build gateway matching the native builder container architecture (naming output gateway_bin to avoid clash with folder name)
RUN CGO_ENABLED=0 go build -o gateway_bin ./gateway/cmd/gateway/main.go

EXPOSE 8080
CMD ["./gateway_bin"]
