# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o tt-exporter cmd/tt-exporter/main.go

# Final stage
FROM alpine:latest

# Install runtime dependencies (tt-smi needs to be available in the host/container environment)
# Note: tt-smi usually requires access to the host's Tenstorrent driver and devices.
RUN apk add --no-cache ca-certificates

WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder /app/tt-exporter .

# Expose the metrics port
EXPOSE 9400

# Run the exporter
ENTRYPOINT ["./tt-exporter"]
