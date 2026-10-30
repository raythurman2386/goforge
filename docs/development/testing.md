# Testing

GoForge uses Go's standard testing package with table-driven tests and race detection.

## Running Tests

```bash
# Run all tests with race detector
make test

# Run tests without race detector (faster)
make test-short

# Run tests with coverage report
make test-coverage
# Open coverage.html in your browser

# Run a specific test
go test -v -run TestFunctionName ./internal/auth/...

# Run tests in a specific package
go test -v ./internal/docker/...

# Run a subtest
go test -v -run "TestName/subtest_name" ./path/to/package/...
```

## Test Structure

Tests live alongside the code they test in `*_test.go` files:

```
internal/
  auth/
    auth.go
    auth_test.go          # Not present yet -- test gap
    session.go
    session_test.go
    middleware.go
    middleware_test.go
    password.go
    password_test.go
  docker/
    client.go
    client_test.go
    compose.go
    compose_test.go
```

## Writing Tests

### Table-Driven Tests

Follow the table-driven test pattern for multiple cases:

```go
func TestService_CreateProject(t *testing.T) {
    tests := []struct {
        name    string
        input   CreateProjectInput
        wantErr bool
    }{
        {
            name:    "valid project",
            input:   CreateProjectInput{Name: "myapp", Domain: "myapp.example.com"},
            wantErr: false,
        },
        {
            name:    "missing name",
            input:   CreateProjectInput{Domain: "myapp.example.com"},
            wantErr: true,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := svc.CreateProject(ctx, tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("CreateProject() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### Test Naming

- Test functions: `TestTypeName_MethodName` or `TestFunctionName`
- Subtests: descriptive lowercase names
- Test files: `*_test.go` next to the code being tested

### Test Database

Repository tests use a test PostgreSQL database. The test helpers in `internal/database/repositories/helpers_test.go` set up and tear down test databases.

```go
func setupTestDB(t *testing.T) *sql.DB {
    // Creates a test database connection
    // Runs migrations
    // Returns a clean database for each test
}
```

### Mocking

The Docker client uses an interface (`dockerAPI`) that can be mocked for testing:

```go
type mockDockerClient struct {
    // Mock implementations of Docker API methods
}
```

Service tests in `internal/services/instance_test.go` demonstrate the mock pattern.

## Integration tests

Tests that need a real Docker daemon or external binaries carry the `//go:build integration` tag. `make test` and CI do not build them; pass `-tags integration` to run them.

| Test | Package | Needs |
|------|---------|-------|
| `TestGiteaPostStartBootstrap` | `internal/services` | Docker |
| `TestGiteaSSHPublish` | `internal/services` | Docker, `ss` |
| `TestDataServices_AuthAndLabels` | `internal/services` | Docker (set `GOFORGE_IT_DOCKER` to change the `docker` command the `pg-tenant.sh` subtest uses) |
| `TestIntegration_RunAgainstMinIO` | `internal/backup` | Docker, `pg_restore`, `sqlite3`, and a GoForge test database in `DATABASE_URL` |

```bash
# One-click service templates against the real Docker client
go test -tags integration -run TestGiteaPostStartBootstrap ./internal/services/ -count=1 -v

# Backups against throwaway MinIO, Postgres, Gitea, and Vaultwarden containers
DATABASE_URL=postgres://goforge:password@localhost:5432/goforge_test?sslmode=disable \
  go test -tags integration -run Integration ./internal/backup/ -count=1 -v
```

Each test skips when Docker or a required binary is missing.

Rules for integration tests:

- **Throwaway resources only.** Each test creates its own network, containers, and volumes under random names (networks such as `goforge-it43-*` and `goforge-itdata-*`, backup containers `gfbk-it-*`) and removes them when it ends. It never touches containers it did not create.
- **Never against production.** Do not run them on a Docker host or database that serves real workloads, and never point `DATABASE_URL` at a production database. Use a database whose name says it is for tests.
- **No external services.** Tests use local containers (MinIO instead of a real bucket), never GitHub, Cloudflare, or a cloud provider.
- If your Docker needs `sudo`, run the whole `go test` under it with your `PATH`, for example `sudo -n env "PATH=$PATH" go test -tags integration ...`.

## CI Testing

The CI pipeline runs tests with:

- Race detector enabled (`-race`)
- Coverage reporting to Codecov
- PostgreSQL 16 service container

```bash
# Reproduce CI test environment locally
make check
```

## Test Coverage

Current coverage varies by package. Critical gaps include:

- `internal/auth/auth.go` -- No tests for local authentication
- `internal/auth/github.go` -- No tests for OAuth provider
- `internal/deploy/` -- Limited pipeline testing

Generate a coverage report:

```bash
make test-coverage
# Opens coverage.html showing line-by-line coverage
```
