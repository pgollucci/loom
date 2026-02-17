# Code Quality & Linting

This document describes the comprehensive linting setup for catching "dumb bugs" before they reach production.

## Quick Start

```bash
# Install all linting tools
make lint-install

# Run all linters
make lint

# Run specific linters
make lint-go        # Go code quality checks
make lint-js        # JavaScript checks
make lint-api       # API/frontend validation
make lint-yaml      # YAML validation
make lint-docs      # Documentation structure
```

## What Gets Checked

### 1. Go Code (`make lint-go`)

Uses **golangci-lint** with comprehensive rules:

**Bug Detection:**
- Unchecked errors (`errcheck`)
- Type mismatches (`typecheck`)
- Unused code (`unused`, `unparam`)
- HTTP response bodies not closed (`bodyclose`)
- Nil returns with non-nil errors (`nilnil`, `nilerr`)
- Error wrapping issues (`errorlint`)

**Code Quality:**
- Cyclomatic complexity >15 (`gocyclo`)
- Code simplification opportunities (`gosimple`)
- Performance issues (`gocritic` - performance tag)
- Ineffectual assignments (`ineffassign`)

**Style & Conventions:**
- Formatting (`gofmt`, `goimports`)
- Spelling (`misspell`)
- Error naming conventions (`errname`, `errorlint`)
- Variable/function naming (`revive`)
- Whitespace issues (`whitespace`)

**Configuration:** `.golangci.yml`

### 2. JavaScript Code (`make lint-js`)

Uses **eslint** with rules for:

- **Undefined variables** (`no-undef`) - catches typos like `apiCal` instead of `apiCall`
- **Unused variables/functions** (`no-unused-vars`) - finds dead code
- **Duplicate keys** (`no-dupe-keys`) - prevents object key conflicts
- **Unreachable code** (`no-unreachable`) - finds code after return/throw
- **Invalid typeof** (`valid-typeof`) - catches `typeof x === "strnig"` typos
- **Semicolon enforcement** (`semi`) - consistent style
- **Quote style** (`quotes`) - consistent string quotes

**Configuration:** `.eslintrc.json`

### 3. API/Frontend Validation (`make lint-api`)

Custom validation script that checks:

- **Undefined API endpoints:** Frontend calls to non-existent backend routes
- **Unused backend endpoints:** API routes never called by frontend
- **Naming mismatches:** snake_case vs camelCase field name issues
- **Missing API handlers:** Routes registered but not implemented

**Script:** `scripts/check-api-frontend.sh`

### 4. YAML Validation (`make lint-yaml`)

Validates:
- YAML syntax
- Persona/config file structure
- Required fields in personas

**Implementation:** `cmd/yaml-lint/main.go`

### 5. Documentation (`make lint-docs`)

Checks:
- README presence
- Documentation structure
- Broken internal links

**Script:** `scripts/check-docs-structure.sh`

## Common Issues Caught

### Missing Methods
```go
type MyInterface interface {
    DoSomething() error
}

type MyImpl struct{}

// CAUGHT: Missing DoSomething method
// golangci-lint: type MyImpl does not implement MyInterface
```

### Typos in Variable Names
```javascript
// CAUGHT: 'apiCal' is not defined
apiCal('/api/v1/beads', function(data) {  // Should be: apiCall
    ...
});
```

### API/Frontend Mismatches
```javascript
// Frontend
fetch('/api/v1/projects/details')  // ❌ Endpoint doesn't exist

// Backend only has:
r.HandleFunc("/api/v1/projects/{id}", handler)  // ❌ Different route
```

**CAUGHT:** Frontend calls to undefined endpoint `/api/v1/projects/details`

### Unchecked Errors
```go
// CAUGHT: Error return value not checked
result := doSomething()  // Returns (Result, error)
fmt.Println(result)      // Forgot to check error!

// Should be:
result, err := doSomething()
if err != nil {
    return err
}
```

### Unused Functions
```javascript
// CAUGHT: 'helperFunction' is defined but never used
function helperFunction() {  // Dead code
    ...
}
```

## Integration with Development Workflow

### Pre-commit Hook (Recommended)

Create `.git/hooks/pre-commit`:
```bash
#!/bin/bash
make lint-go || exit 1
make lint-js || exit 1
```

```bash
chmod +x .git/hooks/pre-commit
```

### CI/CD Integration

Add to your CI pipeline:
```yaml
steps:
  - name: Install linters
    run: make lint-install

  - name: Run linters
    run: make lint
```

### Editor Integration

**VS Code:**
- Install `golangci-lint` extension
- Install `ESLint` extension
- Settings will auto-detect `.golangci.yml` and `.eslintrc.json`

**GoLand/IntelliJ:**
- Preferences → Tools → File Watchers → Add golangci-lint
- Preferences → Languages → JavaScript → Code Quality Tools → ESLint

## Suppressing False Positives

### Go - Inline Comments
```go
//nolint:errcheck // Intentionally ignoring error from Close()
defer file.Close()
```

### JavaScript - Inline Comments
```javascript
/* eslint-disable-next-line no-undef */
const external = ExternalLib.something();  // Loaded via <script> tag
```

### Configuration-based Exclusions

Edit `.golangci.yml` or `.eslintrc.json` to exclude specific patterns.

## Troubleshooting

### golangci-lint Not Found
```bash
# Ensure GOPATH/bin is in PATH
export PATH="$PATH:$(go env GOPATH)/bin"

# Or reinstall
make lint-install
```

### eslint Version Issues
```bash
# Check version
eslint --version

# May need to update .eslintrc.json for older versions
# Change "es2021" → "es6"
```

### API Validation Shows False Positives

The API validator uses pattern matching and may miss:
- Dynamic routes built at runtime
- Routes in external packages
- Template string interpolation in fetch calls

These are warnings, not errors. Review and adjust `scripts/check-api-frontend.sh` filters as needed.

## Maintenance

### Updating golangci-lint
```bash
# Check current version
golangci-lint --version

# Update to latest
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
    sh -s -- -b $(go env GOPATH)/bin
```

### Updating eslint
```bash
npm install -g eslint@latest
```

### Adjusting Strictness

**Too many false positives?**
- Add exclusions to `.golangci.yml` or `.eslintrc.json`
- Disable specific rules in configuration files

**Not catching enough?**
- Enable additional linters in `.golangci.yml`
- Add more rules to `.eslintrc.json`
- Enhance `scripts/check-api-frontend.sh` validation logic

## Benefits

✅ **Catches bugs before runtime** - Undefined variables, missing methods, type errors
✅ **Enforces consistency** - Code style, naming conventions, error handling
✅ **Improves code quality** - Complexity checks, dead code detection, best practices
✅ **Prevents regressions** - API contract validation, interface compliance
✅ **Faster debugging** - Issues caught at lint-time, not in production

## See Also

- [golangci-lint documentation](https://golangci-lint.run/)
- [ESLint rules](https://eslint.org/docs/rules/)
- `make help` for all available commands
