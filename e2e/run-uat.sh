#!/bin/bash
# Run agenthub UAT tests end-to-end
# Usage: ./run-uat.sh
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
DATA_DIR="/tmp/ah-uat-$$"
PORT=8787
ADMIN_KEY="test-admin-key-$$"

echo "=== Building agenthub-server ==="
cd "$REPO_DIR"
go build -o /tmp/agenthub-server-$$ ./cmd/agenthub-server

echo "=== Starting server on :$PORT (data: $DATA_DIR) ==="
/tmp/agenthub-server-$$ \
  --admin-key "$ADMIN_KEY" \
  --data "$DATA_DIR" \
  --listen ":$PORT" &
SERVER_PID=$!

# Wait for server to be ready
for i in $(seq 1 20); do
  if curl -s "http://localhost:$PORT/api/health" | grep -q ok; then
    echo "Server ready."
    break
  fi
  sleep 0.25
done

echo "=== Installing Playwright ==="
cd "$SCRIPT_DIR"
npm install --silent
npx playwright install chromium --with-deps 2>/dev/null || npx playwright install chromium

echo "=== Running UAT tests ==="
BASE_URL="http://localhost:$PORT" ADMIN_KEY="$ADMIN_KEY" npx playwright test --reporter=list
EXIT_CODE=$?

echo "=== Cleaning up ==="
kill $SERVER_PID 2>/dev/null || true
rm -rf "$DATA_DIR" /tmp/agenthub-server-$$

exit $EXIT_CODE
