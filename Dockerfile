# Build stage
FROM golang:1.25-alpine AS builder

ARG GITHUB_TOKEN

# Install build dependencies (including gcc for CGO/sqlite3 and icu-dev for beads)
RUN apk add --no-cache git ca-certificates tzdata gcc g++ musl-dev openssh-client icu-dev wget

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod ./
COPY go.sum ./

# Download dependencies
RUN go mod download

# Install bd CLI for bead operations (build from source with CGO for Dolt support)
# Building from main branch to get latest Dolt support with CGO enabled
RUN git clone --depth 1 https://github.com/steveyegge/beads.git /tmp/beads && \
    cd /tmp/beads && \
    CGO_ENABLED=1 go build -tags dolt -o /go/bin/bd ./cmd/bd && \
    chmod +x /go/bin/bd && \
    rm -rf /tmp/beads

# Install Dolt binary for version-controlled beads backend
RUN if [ -n "$GITHUB_TOKEN" ]; then \
        AUTH_HEADER="Authorization: token ${GITHUB_TOKEN}"; \
    fi; \
    DOLT_VERSION=$(wget -qO- ${AUTH_HEADER:+--header="$AUTH_HEADER"} https://api.github.com/repos/dolthub/dolt/releases/latest | grep tag_name | cut -d '"' -f 4) && \
    wget -q "https://github.com/dolthub/dolt/releases/download/${DOLT_VERSION}/dolt-linux-amd64.tar.gz" && \
    tar -xzf dolt-linux-amd64.tar.gz && \
    mv dolt-linux-amd64/bin/dolt /go/bin/dolt && \
    rm -rf dolt-linux-amd64 dolt-linux-amd64.tar.gz

# Copy source code
COPY . .

# Build the application with CGO enabled for sqlite3
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-w -s" \
    -o loom \
    ./cmd/loom

# Runtime stage
FROM alpine:latest

# Install runtime dependencies including git, openssh, wget, and C++ libs for bd with CGO
RUN apk add --no-cache ca-certificates tzdata git openssh-client wget libstdc++ libgcc icu-libs

# Create non-root user
RUN addgroup -g 1000 loom && \
    adduser -D -u 1000 -G loom loom

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/loom /app/loom

# Copy bd CLI (v0.50.3 pre-built binary)
COPY --from=builder /go/bin/bd /usr/local/bin/bd

# Copy dolt binary for version-controlled beads backend
COPY --from=builder /go/bin/dolt /usr/local/bin/dolt

# Copy config file
COPY --from=builder /build/config.yaml /app/config.yaml

# Copy personas directory
COPY --from=builder /build/personas /app/personas

# Copy workflows directory
COPY --from=builder /build/workflows /app/workflows

# Copy web static files
COPY --from=builder /build/web/static /app/web/static

# Projects (including loom-self) will be cloned at runtime, not copied at build time
# No more COPY .beads or .git - git-centric architecture

# Copy scripts (entrypoint + beads schema SQL)
COPY --from=builder /build/scripts /app/scripts

# Copy bootstrap.local if it exists (provider registration)
# This file is gitignored - use wildcard to make it optional
COPY --from=builder /build/bootstrap.local* /app/
RUN chmod +x /app/scripts/entrypoint.sh

# Create SSH directory for mounted keys and set permissions
RUN mkdir -p /home/loom/.ssh && \
    chown -R loom:loom /home/loom/.ssh && \
    chmod 700 /home/loom/.ssh

# Create data directory for SQLite database and project clones
RUN mkdir -p /app/data/projects /app/data/keys && chown -R loom:loom /app/data

# Change ownership
RUN chown -R loom:loom /app

# Switch to non-root user
USER loom

# Configure git identity and SSH
RUN git config --global user.name "Loom Agent" && \
    git config --global user.email "loom@localhost" && \
    git config --global core.sshCommand "ssh -o UserKnownHostsFile=/home/loom/.ssh/known_hosts -o StrictHostKeyChecking=accept-new"

# Expose port (if needed in future)
EXPOSE 8080

# Set entrypoint (starts Dolt server, then loom)
ENTRYPOINT ["/app/scripts/entrypoint.sh"]
