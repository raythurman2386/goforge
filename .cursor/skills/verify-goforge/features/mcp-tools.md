# GoForge MCP tools

`goforge-mcp` speaks MCP over stdio and calls `/api/v1` with a scoped token. A client sees 23 read tools, plus 14 write tools when `GOFORGE_MCP_ALLOW_WRITES=true`. Write tools are dry runs by default, need `approval_ref` to run, and refuse protected services for stop, config edit, and image update unless the token has the `protected` scope.

## Sub-features

- `mcp-read-inventory` lists 23 read-only tools when writes are off.
- `mcp-write-inventory` lists 37 tools when writes are on, each write tool with `dry_run` and `approval_ref` inputs, and no project, service, or volume delete, no prune, and no secret-reveal tool. `delete_env_var` and `unlink_service` are the only deleting tools, and they are gated writes like the rest.
- `mcp-read-call` returns the API JSON for `whoami` and `list_services`.
- `mcp-dry-run` returns a plan for `restart_service` without `dry_run`, and nothing changes.
- `mcp-protected-refusal` returns an error result for `stop_container` or `update_service_config` on a protected service with a `write` token.

## How to get to it (user POV)

- Create a token as in [api-v1.md](./api-v1.md).
- Build the binary with `make build-mcp`.
- Start `bin/goforge-mcp` with `GOFORGE_MCP_URL=http://127.0.0.1:8080` and `GOFORGE_MCP_TOKEN_FILE` pointing at the token file. An MCP client sends `initialize`, `tools/list`, and `tools/call` over its stdin.

## Driving it with verify.sh

Preconditions:

- The [api-v1.md](./api-v1.md) preconditions, with `token-read`, `token-write`, and the protected `verify-pg` service in place.
- `make build-mcp` has produced `bin/goforge-mcp`.

- **Inventory.** Pipe `initialize`, `notifications/initialized`, and `tools/list` JSON-RPC lines into `GOFORGE_MCP_TOKEN_FILE=/tmp/goforge-verify/state/token-read bin/goforge-mcp`. The list has 23 tools, each with `readOnlyHint` true. With `GOFORGE_MCP_ALLOW_WRITES=true` it has 37, and every write tool's `inputSchema.properties` has `dry_run` and `approval_ref`.
- **Read call.** `tools/call` `whoami` returns text holding `"scopes":["read"]`.
- **Dry run.** With `token-write` and writes on, `tools/call` `restart_service` with `{"service_id":"$ID"}` returns `"dry_run": true` and the container's `StartedAt` is unchanged.
- **Protected.** With `token-write`, `tools/call` `update_service_config` with `{"service_id":"$ID","config":{"name":"verify-pg-renamed"}}` returns `isError` true and text naming `protected`. `stop_container` with `{"container_id":"<verify-pg docker id>"}` does the same.
- **Proof.** Write the tool counts, and per call the tool name, `isError`, and `dry_run`, to `/tmp/goforge-verify/proof/mcp-verdict.txt`. Never write the token.

## Gotchas

- Keep stdin open until the replies arrive, for example `( printf '%s\n' ...; sleep 2 ) | bin/goforge-mcp`. At end of input the server exits and can drop replies still in flight.
- stdout is the MCP stream. Logs go to stderr; do not mix them when parsing.
- The MCP refuses protected actions itself first (its own `GOFORGE_MCP_PROTECTED` list plus the server's list from `whoami`). The server checks again, so both layers must agree.
- `restart_service` and `restart_container` are allowed on protected services.
- API errors arrive as tool results with `isError` true, not as JSON-RPC errors.
- Tool inputs are `service_id` for service tools and `container_id` for container tools, never `id`. The schemas set `additionalProperties: false`, so a wrong name is rejected before any API call.
