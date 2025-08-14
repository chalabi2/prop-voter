# Build stage
FROM golang:1.21-alpine AS builder

# Install git and ca-certificates for dependency fetching
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o prop-voter ./cmd/prop-voter

# Final stage
FROM alpine:latest

# Install required packages
RUN apk --no-cache add ca-certificates sqlite

# Create app user
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# Set working directory
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/prop-voter .

# Copy config example
COPY --from=builder /app/config.example.yaml .

# Create directories for data
RUN mkdir -p /app/data && \
    chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose any ports if needed (optional, since this is a bot)
# EXPOSE 8080

# Set default command
CMD ["./prop-voter", "-config", "/app/config.yaml"]

# Health check (optional)
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD pgrep prop-voter || exit 1