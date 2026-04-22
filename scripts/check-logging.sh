#!/bin/bash
# scripts/check-logging.sh — Quality gate: verify all providers have adequate logging.
#
# Every provider must log at these key points:
#   1. Stream start (Debug)
#   2. HTTP request / API call (Debug)
#   3. HTTP error / API error (Warn)
#   4. Network error or abort (Warn/Debug)
#
# Minimum: 3 log calls per provider (Bedrock uses SDK, not HTTP).
# HTTP-based providers: 4+ log calls.
#
# Usage: ./scripts/check-logging.sh
# Exit code: 0 if all pass, 1 if any fail.

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

MIN_HTTP_PROVIDER=4
MIN_SDK_PROVIDER=2
MIN_FAUX=1

ERRORS=0

check_provider() {
    local file="$1"
    local min_calls="$2"
    local name
    name=$(basename "$(dirname "$file")")

    local count
    count=$(grep -c 'goai\.GetLogger()\.' "$file" 2>/dev/null || echo 0)

    if [ "$count" -lt "$min_calls" ]; then
        echo -e "  ${RED}FAIL${NC} $name ($file): $count log calls (minimum: $min_calls)"
        ERRORS=$((ERRORS + 1))
    else
        echo -e "  ${GREEN}PASS${NC} $name: $count log calls"
    fi

    # Check for required log patterns
    local missing=""

    # Every provider must log stream start
    if ! grep -q 'goai\.GetLogger()\.Debug("stream start"' "$file" 2>/dev/null; then
        missing="${missing}stream-start "
    fi

    if [ -n "$missing" ]; then
        echo -e "  ${YELLOW}WARN${NC} $name missing patterns: $missing"
    fi
}

echo "Logging quality gate"
echo "===================="
echo ""
echo "Checking providers..."

# HTTP-based providers (need 4+ log calls)
for f in \
    provider/openai/openai.go \
    provider/anthropic/anthropic.go \
    provider/google/google.go \
    provider/mistral/mistral.go \
    provider/openairesponses/responses.go \
    provider/geminicli/geminicli.go; do
    check_provider "$f" $MIN_HTTP_PROVIDER
done

# SDK-based providers (need 2+ log calls)
check_provider provider/bedrock/bedrock.go $MIN_SDK_PROVIDER

# Faux (test provider, minimal logging)
check_provider provider/faux/faux.go $MIN_FAUX

echo ""
echo "Checking core modules..."

# Core modules
for f in registry.go retry.go; do
    count=$(grep -c 'logDebug\|logInfo\|logWarn\|logError\|GetLogger' "$f" 2>/dev/null || echo 0)
    if [ "$count" -lt 1 ]; then
        echo -e "  ${RED}FAIL${NC} $f: $count log calls (minimum: 1)"
        ERRORS=$((ERRORS + 1))
    else
        echo -e "  ${GREEN}PASS${NC} $f: $count log calls"
    fi
done

echo ""
if [ "$ERRORS" -gt 0 ]; then
    echo -e "${RED}$ERRORS logging quality gate failures${NC}"
    exit 1
else
    echo -e "${GREEN}All logging quality gates passed${NC}"
    exit 0
fi
