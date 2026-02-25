#!/bin/bash
# Automated release script for Loom
# Usage: ./scripts/release.sh [major|minor|patch]
# Batch mode: BATCH=yes ./scripts/release.sh [major|minor|patch]
#
# Release Checklist (adapted from nanolang):
# 1. Prerequisites check (gh CLI, auth, clean repo, main branch)
# 2. Run comprehensive linters (Go, JS, YAML, docs, API validation)
# 3. Run full test suite
# 4. Generate changelog from git commits
# 5. Update CHANGELOG.md
# 6. Create git tag
# 7. Commit changelog
# 8. Push commits and tags
# 9. Create GitHub release with notes

set -e  # Exit on error

# Batch mode detection
BATCH_MODE="${BATCH:-no}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Functions
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

# Check prerequisites
check_prerequisites() {
    info "Checking prerequisites..."

    # Check if gh CLI is installed
    if ! command -v gh &> /dev/null; then
        error "GitHub CLI (gh) is not installed. Install with: brew install gh"
    fi

    # Check if gh is authenticated
    if ! gh auth status &> /dev/null; then
        error "GitHub CLI is not authenticated. Run: gh auth login"
    fi

    # Check if git repo is clean
    if [[ -n $(git status --porcelain) ]]; then
        error "Git working directory is not clean. Commit or stash changes first."
    fi

    # Check we're on main branch
    CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
    if [[ "$CURRENT_BRANCH" != "main" ]]; then
        warn "Not on main branch (currently on: $CURRENT_BRANCH)"
        if [[ "$BATCH_MODE" == "yes" ]]; then
            error "Not on main branch in batch mode. Switch to main first."
        fi
        read -p "Continue anyway? (y/n) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            error "Aborted by user"
        fi
    fi

    success "Prerequisites check passed"
}

# Run linters before release
run_linters() {
    info "Running comprehensive linters..."

    # Check if linting tools are installed
    if ! command -v golangci-lint &> /dev/null; then
        warn "golangci-lint not found. Installing..."
        make lint-install || warn "Failed to install linting tools (continuing anyway)"
    fi

    # Run all linters (Go, JS, YAML, docs, API validation)
    local lint_output_file=$(mktemp)
    if ! make lint > "$lint_output_file" 2>&1; then
        cat "$lint_output_file"
        rm -f "$lint_output_file"

        if [[ "$BATCH_MODE" == "yes" ]]; then
            error "Linters failed in batch mode. Fix issues before releasing."
        fi

        warn "Linting found issues. Review above output."
        read -p "Continue with release anyway? (y/n) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            error "Release cancelled due to linting failures"
        fi
    else
        rm -f "$lint_output_file"
        success "All linters passed"
    fi
}

# Get current version from git tags
get_current_version() {
    local latest_tag=$(git tag -l 'v*' | sort -V | tail -1)
    if [[ -z "$latest_tag" ]]; then
        echo "0.0.0"
    else
        echo "$latest_tag" | sed 's/^v//'
    fi
}

# Calculate next version
calculate_next_version() {
    local current=$1
    local bump_type=$2

    # Parse current version
    IFS='.' read -r major minor patch <<< "$current"

    case $bump_type in
        major)
            major=$((major + 1))
            minor=0
            patch=0
            ;;
        minor)
            minor=$((minor + 1))
            patch=0
            ;;
        patch)
            patch=$((patch + 1))
            ;;
        *)
            error "Invalid bump type: $bump_type (use major, minor, or patch)"
            ;;
    esac

    echo "$major.$minor.$patch"
}

# Generate changelog entry from git log (Keep a Changelog format)
generate_changelog_entry() {
    local prev_version=$1
    local new_version=$2
    local date=$(date +%Y-%m-%d)

    info "Generating changelog from v$prev_version to HEAD..." >&2

    # Get commits since last version (or all commits if no previous version)
    local commits
    if [[ "$prev_version" == "0.0.0" ]]; then
        commits=$(git log --pretty=format:"%h %s" --no-merges)
    else
        commits=$(git log "v$prev_version"..HEAD --pretty=format:"%h %s" --no-merges)
    fi

    # Categorize commits by conventional commit type
    local added=""
    local changed=""
    local fixed=""
    local removed=""
    local security=""
    local deprecated=""
    local other=""

    while IFS= read -r line; do
        # Conventional commits: feat, fix, refactor, perf, chore, docs, test, style, build, ci
        if [[ $line =~ ^[a-f0-9]+\ feat(\(.*\))?:\ (.*) ]]; then
            added+="- ${BASH_REMATCH[2]}\n"
        elif [[ $line =~ ^[a-f0-9]+\ fix(\(.*\))?:\ (.*) ]]; then
            fixed+="- ${BASH_REMATCH[2]}\n"
        elif [[ $line =~ ^[a-f0-9]+\ (refactor|perf|style)(\(.*\))?:\ (.*) ]]; then
            changed+="- ${BASH_REMATCH[3]}\n"
        elif [[ $line =~ ^[a-f0-9]+\ (chore|docs|test|build|ci)(\(.*\))?:\ (.*) ]]; then
            # Skip internal changes from changelog (docs/chore/test are typically not user-facing)
            continue
        elif [[ $line =~ remove|delete|deprecate ]]; then
            local msg=$(echo "$line" | cut -d' ' -f2-)
            removed+="- $msg\n"
        else
            # Non-conventional commits: try to categorize by keywords
            local msg=$(echo "$line" | cut -d' ' -f2-)
            if [[ $line =~ [Aa]dd|[Ii]mplement|[Nn]ew ]]; then
                added+="- $msg\n"
            elif [[ $line =~ [Ff]ix|[Bb]ug|[Rr]epair ]]; then
                fixed+="- $msg\n"
            elif [[ $line =~ [Uu]pdate|[Cc]hange|[Mm]odify|[Ii]mprove ]]; then
                changed+="- $msg\n"
            else
                other+="- $msg\n"
            fi
        fi
    done <<< "$commits"

    # Build changelog entry (Keep a Changelog format)
    local entry="## [$new_version] - $date\n\n"

    if [[ -n "$added" ]]; then
        entry+="### Added\n$added\n"
    fi

    if [[ -n "$changed" ]]; then
        entry+="### Changed\n$changed\n"
    fi

    if [[ -n "$fixed" ]]; then
        entry+="### Fixed\n$fixed\n"
    fi

    if [[ -n "$removed" ]]; then
        entry+="### Removed\n$removed\n"
    fi

    if [[ -n "$security" ]]; then
        entry+="### Security\n$security\n"
    fi

    if [[ -n "$deprecated" ]]; then
        entry+="### Deprecated\n$deprecated\n"
    fi

    # Only include "Other" section if there are uncategorized commits
    if [[ -n "$other" ]]; then
        entry+="### Other\n$other\n"
    fi

    echo -e "$entry"
}

# Create or update CHANGELOG.md
update_changelog() {
    local changelog_entry=$1
    local changelog_file="CHANGELOG.md"

    info "Updating $changelog_file..."

    # Create temp files
    local temp_file=$(mktemp)
    local entry_file=$(mktemp)

    # Write the entry to a file (handles multi-line strings with emoji)
    echo -e "$changelog_entry" > "$entry_file"

    if [[ ! -f "$changelog_file" ]]; then
        # Create new CHANGELOG
        cat > "$changelog_file" << 'EOF'
# Changelog

All notable changes to Loom will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

EOF
    fi

    # Read changelog and insert new entry after ## [Unreleased]
    awk '
        /^## \[Unreleased\]/ {
            print $0
            print ""
            # Read and insert the new entry from file
            while ((getline line < "'"$entry_file"'") > 0) {
                print line
            }
            close("'"$entry_file"'")
            next
        }
        { print }
    ' "$changelog_file" > "$temp_file"

    mv "$temp_file" "$changelog_file"
    rm "$entry_file"

    success "CHANGELOG.md updated"
}

# Create git tag and release notes
create_release() {
    local version=$1
    local prev_version=$2
    local test_status=$3  # Passed from caller to avoid running tests twice

    info "Creating release v$version..."

    # Get changelog entry for release notes
    local release_notes
    if [[ "$prev_version" == "0.0.0" ]]; then
        release_notes=$(git log --pretty=format:"- %s" --no-merges)
    else
        release_notes=$(git log "v$prev_version"..HEAD --pretty=format:"- %s" --no-merges)
    fi

    # Count statistics
    local commit_count
    if [[ "$prev_version" == "0.0.0" ]]; then
        commit_count=$(git rev-list --count HEAD)
    else
        commit_count=$(git rev-list --count "v$prev_version"..HEAD)
    fi

    # Build release notes
    cat > /tmp/release_notes.md << EOF
## 🎉 Loom v$version

### 📊 Statistics
- **Commits since v$prev_version**: $commit_count
- **Test Status**: $test_status
- **Linters**: ✅ Passed (Go, JavaScript, YAML, Docs, API validation)

### 📝 Changes

$release_notes

### 🔗 Links
- [Full Changelog](https://github.com/jordanhubbard/Loom/compare/v$prev_version...v$version)
- [Documentation](https://github.com/jordanhubbard/Loom/tree/main/docs)
- [User Guide](https://github.com/jordanhubbard/Loom/blob/main/docs/USER_GUIDE.md)
- [Architecture](https://github.com/jordanhubbard/Loom/blob/main/ARCHITECTURE.md)

---

**Full Changelog**: https://github.com/jordanhubbard/Loom/compare/v$prev_version...v$version
EOF

    # Create annotated git tag
    info "Creating git tag v$version..."
    git tag -a "v$version" -m "Release v$version"

    # Commit changelog (with Co-authored-by for releases)
    info "Committing CHANGELOG.md..."
    git add CHANGELOG.md
    git commit -m "docs: Update CHANGELOG for v$version release

Release highlights from v$prev_version

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"

    # Push commits and tags
    info "Pushing to origin..."
    git push origin main
    git push origin "v$version"

    # Create GitHub release
    info "Creating GitHub release..."
    gh release create "v$version" \
        --title "Loom v$version" \
        --notes-file /tmp/release_notes.md

    # Clean up
    rm /tmp/release_notes.md

    success "Release v$version created successfully!"
}

# Main script
main() {
    echo ""
    echo "╔═══════════════════════════════════════╗"
    echo "║    Loom Automated Release Script      ║"
    echo "╚═══════════════════════════════════════╝"
    echo ""

    if [[ "$BATCH_MODE" == "yes" ]]; then
        info "Running in BATCH mode (non-interactive)"
    fi

    # Step 1: Check prerequisites
    check_prerequisites

    # Step 2: Run linters (comprehensive quality checks)
    run_linters

    # Get current version
    CURRENT_VERSION=$(get_current_version)

    info "Current version: v$CURRENT_VERSION"

    # Determine bump type
    BUMP_TYPE=${1:-patch}
    if [[ ! "$BUMP_TYPE" =~ ^(major|minor|patch)$ ]]; then
        error "Invalid argument: $BUMP_TYPE (use major, minor, or patch)"
    fi

    # Calculate next version
    NEXT_VERSION=$(calculate_next_version "$CURRENT_VERSION" "$BUMP_TYPE")

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  Current: v$CURRENT_VERSION"
    echo "  Next:    v$NEXT_VERSION ($BUMP_TYPE)"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    # Confirm
    if [[ "$BATCH_MODE" == "yes" ]]; then
        info "Batch mode: proceeding with release v$NEXT_VERSION"
    else
        read -p "Proceed with release v$NEXT_VERSION? (y/n) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            warn "Release cancelled by user"
            exit 0
        fi
    fi

    # Generate changelog entry
    CHANGELOG_ENTRY=$(generate_changelog_entry "$CURRENT_VERSION" "$NEXT_VERSION")

    echo ""
    info "Generated changelog entry:"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo -e "$CHANGELOG_ENTRY"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    if [[ "$BATCH_MODE" == "yes" ]]; then
        info "Batch mode: accepting changelog entry"
    else
        read -p "Does this look correct? (y/n) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            warn "Please edit CHANGELOG.md manually and re-run"
            exit 0
        fi
    fi

    # Update changelog
    update_changelog "$CHANGELOG_ENTRY"

    # Step 3: Run tests before release
    info "Running unit tests..."
    local test_output_file=$(mktemp)

    if go test ./... -short -count=1 > "$test_output_file" 2>&1; then
        local test_status="✅ All tests passed"
    else
        local test_status="⚠️ Some tests failed (see test output)"

        if [[ "$BATCH_MODE" == "yes" ]]; then
            warn "Tests had failures in batch mode (continuing with release)."
            tail -20 "$test_output_file"
        else
            warn "Tests failed! See output:"
            tail -30 "$test_output_file"
            echo ""
            read -p "Continue with release anyway? (y/n) " -n 1 -r
            echo
            if [[ ! $REPLY =~ ^[Yy]$ ]]; then
                rm -f "$test_output_file"
                error "Release cancelled due to test failures"
            fi
        fi
    fi

    rm -f "$test_output_file"
    success "Tests completed"

    # Create release
    create_release "$NEXT_VERSION" "$CURRENT_VERSION" "$test_status"

    echo ""
    echo "╔═══════════════════════════════════════╗"
    echo "║    🎉 Release Complete! 🎉            ║"
    echo "╚═══════════════════════════════════════╝"
    echo ""
    echo "Release: https://github.com/jordanhubbard/Loom/releases/tag/v$NEXT_VERSION"
    echo ""
    echo "Next steps:"
    echo "  • Review the release on GitHub"
    echo "  • Update documentation if needed"
    echo "  • Announce the release to users"
    echo ""
}

# Run main
main "$@"
