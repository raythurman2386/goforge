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

- `internal/auth/csrf.go` -- No tests for CSRF middleware
- `internal/auth/auth.go` -- No tests for local authentication
- `internal/auth/github.go` -- No tests for OAuth provider
- `internal/deploy/` -- Limited pipeline testing

Generate a coverage report:

```bash
make test-coverage
# Opens coverage.html showing line-by-line coverage
```
