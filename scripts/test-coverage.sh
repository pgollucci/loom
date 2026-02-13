#!/bin/bash
# Test coverage analysis script for Loom
# Generates coverage reports and enforces minimum coverage threshold

set -e

# Configuration
MIN_COVERAGE=${MIN_COVERAGE:-75}
COVERAGE_FILE=${COVERAGE_FILE:-coverage.out}
COVERAGE_HTML=${COVERAGE_HTML:-coverage.html}
COVERAGE_JSON=${COVERAGE_JSON:-coverage.json}

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

success() {
    echo -e "${GREEN}✅ $1${NC}"
}

warn() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

error() {
    echo -e "${RED}❌ $1${NC}"
    exit 1
}

echo ""
echo "╔═══════════════════════════════════════╗"
echo "║    Loom Test Coverage Analysis        ║"
echo "╚═══════════════════════════════════════╝"
echo ""

# Run tests with coverage
info "Running tests with coverage..."
if ! go test -coverprofile="$COVERAGE_FILE" -covermode=atomic ./...; then
    error "Tests failed. Fix failing tests before checking coverage."
fi

success "Tests completed"

# Generate coverage statistics
info "Generating coverage statistics..."

# Get overall coverage percentage
TOTAL_COVERAGE=$(go tool cover -func="$COVERAGE_FILE" | grep total | awk '{print $3}' | sed 's/%//')

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Overall Coverage: ${TOTAL_COVERAGE}%"
echo "  Minimum Required: ${MIN_COVERAGE}%"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Generate HTML report
info "Generating HTML coverage report..."
go tool cover -html="$COVERAGE_FILE" -o "$COVERAGE_HTML"
success "HTML report generated: $COVERAGE_HTML"

# Generate detailed per-package coverage
info "Package-level coverage:"
echo ""
go tool cover -func="$COVERAGE_FILE" | grep -v "total:" | awk '{
    # Extract package and coverage
    split($1, parts, "/")
    pkg = parts[length(parts)-1]
    if (pkg == "") pkg = parts[length(parts)]
    coverage[pkg] += $NF
    count[pkg]++
}
END {
    for (pkg in coverage) {
        avg = coverage[pkg] / count[pkg]
        printf "  %-40s %6.1f%%\n", pkg ":", avg
    }
}' | sort -k2 -rn

echo ""

# Find files with low coverage
info "Files with coverage < ${MIN_COVERAGE}%:"
echo ""
go tool cover -func="$COVERAGE_FILE" | awk -v min="$MIN_COVERAGE" '
    $NF != "total:" {
        # Remove % sign and compare
        cov = $NF
        gsub(/%/, "", cov)
        if (cov + 0 < min + 0 && $1 !~ /_test\.go/) {
            printf "  %-60s %6s\n", $1, $NF
        }
    }
' | head -20

# Check if coverage meets minimum threshold
echo ""
if (( $(echo "$TOTAL_COVERAGE < $MIN_COVERAGE" | bc -l) )); then
    error "Coverage ${TOTAL_COVERAGE}% is below minimum threshold of ${MIN_COVERAGE}%"
else
    success "Coverage ${TOTAL_COVERAGE}% meets minimum threshold of ${MIN_COVERAGE}%"
fi

echo ""
info "Coverage report available at: $COVERAGE_HTML"
info "Open with: open $COVERAGE_HTML"
echo ""
