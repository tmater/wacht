#!/bin/bash
# dev.sh — start server + 2 probes for local testing
# Usage: ./dev.sh
# Logs are written to logs/ — tail -f logs/server.log to follow
# Stop with Ctrl+C

set -e

if [ ! -f go.mod ]; then
    echo "Error: run this script from the wacht project root"
    exit 1
fi

GOBIN=/usr/local/go/bin/go

# Kill anything already on port 8080
lsof -ti:8080 | xargs kill -9 2>/dev/null || true

# Clean previous run artifacts
rm -f wacht.db wacht.db-wal wacht.db-shm
rm -rf logs bin
mkdir -p logs bin

echo "Building binaries..."
$GOBIN build -o bin/wacht-server ./cmd/wacht-server/
$GOBIN build -o bin/wacht-probe ./cmd/wacht-probe/

echo "Starting server... (logs/server.log)"
bin/wacht-server > logs/server.log 2>&1 &
SERVER_PID=$!
sleep 1

echo "Starting probe-eu-west... (logs/probe-eu-west.log)"
bin/wacht-probe --probe-id=probe-eu-west > logs/probe-eu-west.log 2>&1 &
PROBE1_PID=$!

echo "Starting probe-eu-central... (logs/probe-eu-central.log)"
bin/wacht-probe --probe-id=probe-eu-central > logs/probe-eu-central.log 2>&1 &
PROBE2_PID=$!

echo ""
echo "All running. Tailing logs — press Ctrl+C to stop all."
echo ""

cleanup() {
    echo ""
    echo "Stopping..."
    kill $SERVER_PID $PROBE1_PID $PROBE2_PID 2>/dev/null
    exit 0
}
trap cleanup INT

tail -f logs/server.log logs/probe-eu-west.log logs/probe-eu-central.log
