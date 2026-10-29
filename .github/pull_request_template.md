<!-- See CONTRIBUTING.md. Keep one topic per PR. Reference the issue: Fixes #123 -->

## What changed

<!-- What this PR changes and why. Link the issue. -->

-

## Tests

<!-- Tests added or changed, and the commands you ran. -->

- [ ] `make check` passes (fmt-check, vet, lint, test)
- [ ] New or changed behavior has a test that fails without the change
- [ ] Integration tests run if relevant (`go test -tags integration ...`, throwaway containers only)
- [ ] `mkdocs build --strict` passes (if `docs/` or `mkdocs.yml` changed)

## Proof

<!--
What you ran and what you saw: command output, a verify-goforge drive, a curl
transcript, or screenshots for UI changes. Paste or link it here; never commit
proof files. No secrets, tokens, or .env values.
-->

## Docs and changelog

- [ ] `CHANGELOG.md` updated under `## [Unreleased]` (or not user-visible)
- [ ] Docs in `docs/` updated for behavior, config, or API changes
- [ ] New environment variables added to `.env.example` and `docs/configuration/index.md`
- [ ] Migrations are new numbered up/down pairs and `docs/reference/database-schema.md` is updated
- [ ] No secrets in code, logs, `docker.command`, or committed files

## Deferred

<!-- Anything left out on purpose, with a follow-up issue if one exists. -->
