# Build stage (SQLite driver needs CGO)
FROM golang:1.23-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN test -f http/api/health.go || (echo "Missing http/api/health.go in build context" && exit 1)
RUN CGO_ENABLED=1 GOOS=linux go build -o linkup-api ./cmd

# Run stage
FROM alpine:3.19
RUN apk --no-cache add ca-certificates
WORKDIR /app

# Default DB path inside container; override with LINKUP_DATABASE_URL
ENV LINKUP_DATABASE_URL=/data/linkup.db
ENV LINKUP_SERVER_ADDRESS=:8181

COPY --from=builder /app/linkup-api .

# Create dir for SQLite file (can be mounted)
RUN mkdir -p /data

EXPOSE 8181

# Healthcheck uses the contract endpoint (no wget/curl in minimal image: skip or add curl)
HEALTHCHECK --interval=10s --timeout=3s --start-period=2s --retries=3 \
	CMD nc -z localhost 8181 || exit 1

CMD ["./linkup-api"]
