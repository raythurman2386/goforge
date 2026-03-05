# Linting

GoForge uses an extensive linting configuration with 30+ linters to maintain code quality.

## Running Linters

```bash
# Run the full linter suite
make lint

# Run all checks (format + vet + lint + test)
make check

# Check formatting only
make fmt-check

# Run go vet only
make vet
```

## Linter Configuration

The linter configuration lives in `.golangci.yml`. It uses [golangci-lint](https://golangci-lint.run/) which bundles many linters into a single tool.

### Enabled Linters

| Linter | Category | Purpose |
|--------|----------|---------|
| `errcheck` | Correctness | All errors must be handled |
| `gosimple` | Correctness | Simplify code |
| `govet` | Correctness | Reports suspicious constructs |
| `ineffassign` | Correctness | Detects ineffectual assignments |
| `staticcheck` | Correctness | Advanced static analysis |
| `unused` | Correctness | Finds unused code |
| `bodyclose` | Correctness | HTTP response bodies must be closed |
| `contextcheck` | Correctness | Context must be passed correctly |
| `durationcheck` | Correctness | Check duration multiplication |
| `errname` | Style | Check error naming conventions |
| `errorlint` | Style | Use `errors.Is`/`errors.As` |
| `exhaustive` | Correctness | Check exhaustiveness of enum switch |
| `gocognit` | Quality | Cognitive complexity (max: 30) |
| `goconst` | Quality | Repeated string detection |
| `gocritic` | Quality | Opinionated linter |
| `gofmt` | Formatting | Standard Go formatting |
| `goimports` | Formatting | Import organization |
| `goprintffuncname` | Style | Printf-like functions named correctly |
| `gosec` | Security | Security issue detection |
| `misspell` | Quality | Spelling errors in comments/strings |
| `nakedret` | Style | Naked returns in large functions |
| `nilerr` | Correctness | Return nil error after checking err != nil |
| `noctx` | Correctness | HTTP requests without context |
| `nolintlint` | Quality | Check nolint directives |
| `prealloc` | Performance | Slice preallocation suggestions |
| `predeclared` | Quality | Shadowing predeclared identifiers |
| `revive` | Style | Configurable Go linter |
| `rowserrcheck` | Correctness | Check sql.Rows.Err is checked |
| `sqlclosecheck` | Correctness | sql.Rows must be closed |
| `stylecheck` | Style | Style mistakes |
| `tparallel` | Correctness | Detect t.Parallel() misuse |
| `unconvert` | Quality | Unnecessary type conversions |
| `unparam` | Quality | Unused function parameters |
| `whitespace` | Formatting | Whitespace issues |

### Disabled Linters

| Linter | Reason |
|--------|--------|
| `dupl` | Acceptable duplication in repositories |
| `funlen` | Function length (too restrictive) |
| `gochecknoglobals` | No globals (too restrictive for config) |
| `gochecknoinits` | No init functions (sometimes needed) |
| `lll` | Line length (handled by gofmt) |
| `wsl` | Whitespace linter (too opinionated) |

### Excluded Files

- `*_templ.go` -- Generated templ files are excluded from all linting
- Test files have relaxed rules for `errcheck`, `errorlint`, `gocognit`, `dupl`, and `goconst`

## Import Organization

Imports must be grouped in this order:

```go
import (
    // Standard library
    "context"
    "fmt"
    "net/http"

    // Third-party packages
    "github.com/go-chi/chi/v5"
    "github.com/lib/pq"

    // Local packages
    "github.com/raythurman2386/goforge/internal/config"
    "github.com/raythurman2386/goforge/internal/models"
)
```

This is enforced by `goimports` with the local prefix configured in `.golangci.yml`.

!!! note "Local prefix"
    The `.golangci.yml` has `local-prefixes: github.com/raythurman2386/goforge` configured to enforce correct import grouping.

## Formatting

```bash
# Format all Go files
make fmt

# Check formatting without modifying files
make fmt-check
```

GoForge uses `gofmt -s` which applies simplifications in addition to standard formatting.

## Common Lint Errors

### `errcheck`: Error return value not checked

```go
// Bad
file.Close()

// Good
if err := file.Close(); err != nil {
    return fmt.Errorf("failed to close file: %w", err)
}
```

### `errorlint`: Use errors.Is for comparison

```go
// Bad
if err == sql.ErrNoRows {

// Good
if errors.Is(err, sql.ErrNoRows) {
```

### `bodyclose`: HTTP response body not closed

```go
// Bad
resp, err := http.Get(url)
// use resp

// Good
resp, err := http.Get(url)
if err != nil {
    return err
}
defer resp.Body.Close()
```

## Suppressing Lint Warnings

Use `//nolint` directives sparingly and with a reason:

```go
//nolint:gosec // G401: SHA-256 is used for token hashing, not password hashing
hash := sha256.Sum256(token)
```
