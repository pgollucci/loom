# Build stage
FROM golang:1.25-bookworm AS builder

ARG GITHUB_TOKEN

# Install build dependencies (libicu-dev for beads, gcc for bd CLI with Dolt support)
RUN apt-get update && apt-get install -y --no-install-recommends \
    git ca-certificates tzdata gcc g++ libicu-dev openssh-client wget \
    && rm -rf /var/lib/apt/lists/*

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

# Build the main application (CGO not required; postgres driver is pure Go)
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -o loom \
    ./cmd/loom

# Build the project agent for per-project containers
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -o loom-project-agent \
    ./cmd/loom-project-agent

# Build the connectors microservice
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -o connectors-service \
    ./cmd/connectors-service

# Runtime stage — Debian bookworm matches the builder stage (same libicu72 ABI)
FROM debian:bookworm-slim

# Install runtime dependencies including git, openssh, wget, Docker CLI, and C++ libs for bd with CGO
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    ca-certificates tzdata git openssh-client wget libstdc++6 libicu72 docker.io docker-compose \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user and docker group for Docker socket access
# Use GID 988 to match host docker socket (may need adjustment on different systems)
# docker.io creates the docker group; adjust its GID then create the loom user
RUN groupmod -g 988 docker && \
    groupadd -g 1000 loom && \
    useradd -u 1000 -g loom -m -s /bin/bash loom && \
    usermod -aG docker loom

# Set working directory
WORKDIR /app

# Copy binaries from builder
COPY --from=builder /build/loom /app/loom
COPY --from=builder /build/loom-project-agent /app/loom-project-agent
COPY --from=builder /build/connectors-service /app/connectors-service

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
RUN chmod +x /app/scripts/entrypoint.sh

# Install git-askpass-helper for token authentication
COPY scripts/git-askpass-helper.sh /usr/local/bin/git-askpass-helper
RUN chmod +x /usr/local/bin/git-askpass-helper

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
