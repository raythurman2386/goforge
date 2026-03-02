# Contributing to GoForge

Thank you for your interest in contributing to GoForge. This section covers everything you need to know to get started as a contributor.

## Sections

- [Development Setup](setup.md) -- Set up your local development environment
- [Testing](testing.md) -- Running and writing tests
- [Linting](linting.md) -- Code quality and formatting standards
- [Database Migrations](migrations.md) -- Creating and managing schema changes
- [Templates & Frontend](frontend.md) -- Working with templ templates and HTMX

## Contribution Workflow

1. Fork the repository
2. Create a feature branch: `git checkout -b feat/my-feature`
3. Make your changes
4. Run checks: `make check`
5. Commit with conventional commits: `feat(deploy): add blue-green strategy`
6. Push and open a pull request

## Commit Message Convention

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): description
```

| Type | Usage |
|------|-------|
| `feat` | New feature |
| `fix` | Bug fix |
| `docs` | Documentation only |
| `refactor` | Code restructuring without behavior change |
| `test` | Adding or updating tests |
| `chore` | Build, CI, or dependency updates |

**Examples:**

```
feat(deploy): add blue-green deployment strategy
fix(auth): prevent session fixation on login
docs(api): add webhook endpoint documentation
refactor(docker): extract build logic to separate file
test(services): add catalog template parsing tests
chore(deps): update go-github to v60
```

## Code Style

- Run `make fmt` before committing (enforced by CI)
- Follow the naming conventions in [AGENTS.md](https://github.com/raythurman2386/goforge/blob/main/AGENTS.md)
- Handle all errors explicitly -- never ignore them
- Use `context.Context` as the first parameter for I/O functions
- Wrap errors with context: `fmt.Errorf("failed to create project: %w", err)`
