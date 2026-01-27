#!/bin/bash
set -e

echo "📋 Checking documentation structure..."

# Repository rules from AGENTS.md:
# - All documentation goes in docs/
# - All internal AI planning files go in plans/
# - All intermediate object files go in obj/ (never committed)
# - All binaries go in bin/ (never committed)

ERRORS=0

# Allowed markdown files in root (entry points and high-level guides)
ALLOWED_ROOT=(
  "./README.md"
  "./AGENTS.md"
  "./MANUAL.md"
  "./CHANGELOG.md"
  "./CONTRIBUTING.md"
  "./LICENSE.md"
  "./MILESTONES.md"
  "./QUICKSTART.md"
  "./README_OLD.md"
)

# Find all .md files in root (excluding hidden dirs and allowed files)
echo ""
echo "🔍 Checking for misplaced documentation files in root..."

for file in ./*.md; do
  [ -f "$file" ] || continue
  
  # Skip if in allowed list
  allowed=false
  for allowed_file in "${ALLOWED_ROOT[@]}"; do
    if [ "$file" = "$allowed_file" ]; then
      allowed=true
      break
    fi
  done
  
  if [ "$allowed" = false ]; then
    echo "❌ ERROR: Documentation file in root should be in docs/: $file"
    ERRORS=$((ERRORS + 1))
  fi
done

# Check for planning files in wrong location
echo ""
echo "🔍 Checking for misplaced planning files..."

# Planning files should be in plans/ directory
if [ -d "plans" ]; then
  echo "✓ plans/ directory exists"
else
  echo "⚠️  plans/ directory does not exist (will be created when needed)"
fi

# Look for common planning file patterns outside of plans/
find . -maxdepth 2 -type f \( \
  -name "*PLAN*.md" -o \
  -name "*TODO*.md" -o \
  -name "*WIP*.md" -o \
  -name "*DRAFT*.md" -o \
  -name "*ROADMAP*.md" \
\) ! -path "./plans/*" ! -path "./.git/*" ! -path "./docs/*" | while read -r file; do
  echo "⚠️  WARNING: Potential planning file should be in plans/: $file"
done

# Check that detailed docs are in docs/ not root
echo ""
echo "🔍 Checking docs/ directory..."

if [ ! -d "docs" ]; then
  echo "❌ ERROR: docs/ directory is missing!"
  ERRORS=$((ERRORS + 1))
else
  doc_count=$(find docs -name "*.md" -type f | wc -l)
  echo "✓ Found $doc_count documentation files in docs/"
fi

# Report results
echo ""
if [ $ERRORS -eq 0 ]; then
  echo "✅ Documentation structure check passed!"
  exit 0
else
  echo ""
  echo "❌ Found $ERRORS error(s) in documentation structure"
  echo ""
  echo "Repository Rules (from AGENTS.md):"
  echo "  • All documentation goes in docs/"
  echo "  • All internal AI planning files go in plans/"
  echo "  • Root directory: Only entry points (README, AGENTS, MANUAL, etc.)"
  echo ""
  exit 1
fi
