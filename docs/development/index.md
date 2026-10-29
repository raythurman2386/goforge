# Contributing to GoForge

The contributor guide is the root
[CONTRIBUTING.md](https://github.com/raythurman2386/goforge/blob/main/CONTRIBUTING.md).
It covers the house rules, tests, migrations, service templates, docs, and how
to send a change. AI agents start at
[AGENTS.md](https://github.com/raythurman2386/goforge/blob/main/AGENTS.md).

The pages in this section add detail for specific tasks:

- [Development Setup](setup.md): set up a local environment
- [Testing](testing.md): unit tests, integration tests, and coverage
- [Linting](linting.md): formatting and golangci-lint
- [Database Migrations](migrations.md): creating and applying schema changes
- [Templates & Frontend](frontend.md): templ templates and HTMX

Before you push, run `make check`. If you changed `docs/` or `mkdocs.yml`, also
run `mkdocs build --strict`.
