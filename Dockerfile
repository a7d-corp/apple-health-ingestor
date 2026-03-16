FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X main.version=$(git rev-parse --short HEAD 2>/dev/null || echo dev)" \
    -o health-ingestion .

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/health-ingestion .
EXPOSE 8080
ENTRYPOINT ["./health-ingestion"]
