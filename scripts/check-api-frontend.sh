#!/usr/bin/env bash
set -euo pipefail

# Check for API/Frontend mismatches
# Validates that frontend API calls match backend endpoints

RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

ERRORS=0
WARNINGS=0

echo "=== API/Frontend Validation ==="
echo

# Find all API endpoint definitions in Go handlers
echo "📍 Extracting backend API endpoints..."
BACKEND_ENDPOINTS=$(mktemp)
grep -rn "r\.HandleFunc\|r\.Handle\|mux\.HandleFunc" internal/api/ cmd/loom/main.go 2>/dev/null | \
    grep -oE '"/api/[^"]*"' | \
    sort -u | \
    sed 's/"//g' > "$BACKEND_ENDPOINTS" || true

if [ ! -s "$BACKEND_ENDPOINTS" ]; then
    echo -e "${YELLOW}⚠ Warning: No backend API endpoints found${NC}"
    WARNINGS=$((WARNINGS + 1))
fi

echo "Found $(wc -l < "$BACKEND_ENDPOINTS") backend endpoints"
echo

# Find all API calls in JavaScript
echo "📍 Extracting frontend API calls..."
FRONTEND_CALLS=$(mktemp)
grep -rn "fetch(\|axios\.get\|axios\.post\|axios\.put\|axios\.delete" web/static/js/ 2>/dev/null | \
    grep -oE '"/api/[^"?]*' | \
    perl -pe 's/\$\{[^}]*\}/:id/g' | \
    sed 's/"//g' | \
    sort -u > "$FRONTEND_CALLS" || true

if [ ! -s "$FRONTEND_CALLS" ]; then
    echo -e "${YELLOW}⚠ Warning: No frontend API calls found${NC}"
    WARNINGS=$((WARNINGS + 1))
fi

echo "Found $(wc -l < "$FRONTEND_CALLS") frontend API calls"
echo

# Check for frontend calls to non-existent endpoints
echo "🔍 Checking for undefined API endpoints..."
UNDEFINED=$(mktemp)
while IFS= read -r endpoint; do
    # Simple string match (ignoring :param differences)
    base_endpoint=$(echo "$endpoint" | sed 's/:id/PARAM/g' | sed 's/:name/PARAM/g' | sed 's/:project_id/PARAM/g')
    backend_normalized=$(sed 's/:id/PARAM/g; s/:name/PARAM/g; s/:project_id/PARAM/g' "$BACKEND_ENDPOINTS")

    if ! echo "$backend_normalized" | grep -qF "$base_endpoint"; then
        echo "$endpoint" >> "$UNDEFINED"
    fi
done < "$FRONTEND_CALLS"

if [ -s "$UNDEFINED" ]; then
    echo -e "${RED}❌ Frontend calls to undefined endpoints:${NC}"
    while IFS= read -r endpoint; do
        echo -e "${RED}   - $endpoint${NC}"
        # Show where it's used
        grep -rn "$endpoint" web/static/js/ | head -3 | while IFS= read -r line; do
            echo -e "${YELLOW}     $line${NC}"
        done
    done < "$UNDEFINED"
    ERRORS=$((ERRORS + $(wc -l < "$UNDEFINED")))
    echo
fi

# Check for unused backend endpoints
echo "🔍 Checking for unused backend endpoints..."
UNUSED=$(mktemp)
while IFS= read -r endpoint; do
    # Skip health/metrics endpoints
    if echo "$endpoint" | grep -qE "^/api/(health|metrics|status)"; then
        continue
    fi

    # Normalize for comparison (replace :param with PARAM for matching)
    base_endpoint=$(echo "$endpoint" | sed 's/:id/PARAM/g; s/:name/PARAM/g; s/:project_id/PARAM/g')
    frontend_normalized=$(sed 's/:id/PARAM/g; s/:name/PARAM/g; s/:project_id/PARAM/g' "$FRONTEND_CALLS")

    if ! echo "$frontend_normalized" | grep -qF "$base_endpoint"; then
        echo "$endpoint" >> "$UNUSED"
    fi
done < "$BACKEND_ENDPOINTS"

if [ -s "$UNUSED" ]; then
    echo -e "${YELLOW}⚠ Potentially unused backend endpoints:${NC}"
    while IFS= read -r endpoint; do
        echo -e "${YELLOW}   - $endpoint${NC}"
    done < "$UNUSED"
    WARNINGS=$((WARNINGS + $(wc -l < "$UNUSED")))
    echo
fi

# Check for API response field mismatches
echo "🔍 Checking for common field name mismatches..."
FIELD_ERRORS=$(mktemp)

# Check for snake_case in backend vs camelCase in frontend
if grep -rn "json:\"[^\"]*_[^\"]*\"" internal/api/ internal/models/ 2>/dev/null > /dev/null; then
    if grep -rn "\\.\\w*[A-Z]\\w*" web/static/js/ 2>/dev/null > /dev/null; then
        echo -e "${YELLOW}⚠ Warning: Mixed naming conventions detected (snake_case in Go, camelCase in JS)${NC}"
        echo "  This may cause field access errors. Consider standardizing on one convention."
        WARNINGS=$((WARNINGS + 1))
    fi
fi

# Clean up temp files
rm -f "$BACKEND_ENDPOINTS" "$FRONTEND_CALLS" "$UNDEFINED" "$UNUSED" "$FIELD_ERRORS"

# Summary
echo
echo "=== Validation Summary ==="
if [ $ERRORS -gt 0 ]; then
    echo -e "${RED}❌ $ERRORS error(s) found${NC}"
fi
if [ $WARNINGS -gt 0 ]; then
    echo -e "${YELLOW}⚠  $WARNINGS warning(s) found${NC}"
fi
if [ $ERRORS -eq 0 ] && [ $WARNINGS -eq 0 ]; then
    echo -e "${GREEN}✅ All checks passed${NC}"
fi
echo

exit $ERRORS
