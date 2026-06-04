# Build stage
FROM golang:1.26-bookworm AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o tt-exporter cmd/tt-exporter/main.go

# Final stage
FROM python:3.14-slim-bookworm

WORKDIR /root/

# Install system dependencies for tt-smi
RUN apt-get update && apt-get install -y --no-install-recommends \
    libatomic1 \
    pciutils \
    && rm -rf /var/lib/apt/lists/*

# Install tt-smi and its dependencies
RUN pip install --no-cache-dir tt-smi==5.2.0

# Copy the binary from the builder stage
COPY --from=builder /app/tt-exporter .

# Expose the metrics port
EXPOSE 9400

# Run the exporter
ENTRYPOINT ["./tt-exporter"]
