#!/bin/sh
set -e

# Entrypoint script for loom container
# Starts Dolt SQL server for beads backend, then starts loom

BEADS_DIR="/app/data/projects/loom-self/.beads"
BEADS_DOLT_DIR="$BEADS_DIR/dolt/beads"
DOLT_PORT="${DOLT_PORT:-3307}"

start_dolt() {
    # Configure Dolt identity if not set
    dolt config --global --get user.name >/dev/null 2>&1 || \
        dolt config --global --add user.name "loom"
    dolt config --global --get user.email >/dev/null 2>&1 || \
        dolt config --global --add user.email "loom@localhost"

    # If the Dolt repo doesn't exist but the beads dir does, initialize it
    if [ -d "$BEADS_DIR" ] && [ ! -d "$BEADS_DOLT_DIR/.dolt" ]; then
        echo "[entrypoint] Initializing Dolt database at $BEADS_DOLT_DIR..."
        mkdir -p "$BEADS_DOLT_DIR"
        cd "$BEADS_DOLT_DIR"
        dolt init --name loom --email loom@localhost
        echo "[entrypoint] Dolt database initialized"
    fi

    # Wait for the beads directory to exist (created after git clone on first run)
    if [ ! -d "$BEADS_DOLT_DIR/.dolt" ]; then
        echo "[entrypoint] Beads directory not yet available (waiting for first clone)"
        echo "[entrypoint] Dolt server skipped for this startup"
        return 1
    fi

    echo "[entrypoint] Starting Dolt SQL server on port $DOLT_PORT..."
    cd "$BEADS_DOLT_DIR"

    # Start Dolt SQL server in background
    dolt sql-server \
        --host 0.0.0.0 \
        --port "$DOLT_PORT" \
        &

    DOLT_PID=$!
    echo "[entrypoint] Dolt SQL server started (PID $DOLT_PID)"

    # Wait for Dolt to be ready (up to 10 seconds)
    for i in $(seq 1 20); do
        if dolt sql -q "SELECT 1" >/dev/null 2>&1; then
            echo "[entrypoint] Dolt SQL server ready on port $DOLT_PORT"
            return 0
        fi
        sleep 0.5
    done

    echo "[entrypoint] Warning: Dolt SQL server did not become ready in time"
    return 0
}

# Try to start Dolt (non-fatal if it fails)
start_dolt || echo "[entrypoint] Continuing without Dolt server"

# Return to app directory and start loom
cd /app
exec /app/loom "$@"
