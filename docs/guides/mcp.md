# MCP Server

`goforge-mcp` is a [Model Context Protocol](https://modelcontextprotocol.io)
server for GoForge. An agent uses it to read projects, deployments, services,
containers, logs, and the audit log, and, when allowed, to run gated changes.

- It speaks MCP over **stdio** only. There is no MCP HTTP listener; reach it
  over SSH.
- It calls the [API v1](../reference/api-v1.md) with a scoped bearer token.
  It has no Docker socket and no database connection, so every check (scope,
  ownership, protected services, approval, audit) runs in GoForge.
- There is no tool that reveals a secret.

## 1. Create a token

Sign in as the admin user and open **Settings → Profile → API Tokens →
Generate Token**.

| For | Check |
|-----|-------|
| Read-only agent | `read` |
| Agent that may deploy, restart, and edit variables | `write` |
| Agent that may also stop, reconfigure, or re-image protected services | `write` and `protected` |

Copy the token when it is shown; it is shown once. Keep it in a file readable
only by the user that runs the MCP server, for example
`~/.config/goforge-mcp/token` with mode `0600`.

## 2. Protect the shared services

Set the server-side list on the GoForge container (comma-separated service
instance IDs, see [Configuration](../configuration/index.md#api-v1-and-mcp)):

```bash
GOFORGE_PROTECTED_SERVICES=<shared-pg-id>,<shared-redis-id>,<gitea-id>,<vaultwarden-id>,<solidcraft-id>
```

It takes effect when the GoForge container next starts. Until then the
server list is empty and only the MCP's own list (below) protects them.

## 3. Build

```bash
make build-mcp            # bin/goforge-mcp
make docker-build-mcp     # image goforge-mcp:latest (Dockerfile target "mcp")
```

## 4. Configure

| Variable | Default | Meaning |
|----------|---------|---------|
| `GOFORGE_MCP_URL` | `http://127.0.0.1:8080` (image: `http://goforge:8080`) | GoForge base URL, without `/api/v1` |
| `GOFORGE_MCP_TOKEN` | | API token |
| `GOFORGE_MCP_TOKEN_FILE` | | File holding the token, used when `GOFORGE_MCP_TOKEN` is empty |
| `GOFORGE_MCP_ALLOW_WRITES` | off | `true` registers the write tools. Off: read tools only |
| `GOFORGE_MCP_PROTECTED` | empty | Extra protected service instance IDs, merged with the server's list |

## 5. Connect over SSH

The client starts the server with `ssh -T` and talks over its stdin and
stdout. `-T` keeps a terminal from mangling the stream.

Host binary (GoForge publishes `127.0.0.1:8080` on the host):

```json
{
  "mcpServers": {
    "goforge": {
      "command": "ssh",
      "args": ["-T", "minis",
               "GOFORGE_MCP_TOKEN_FILE=$HOME/.config/goforge-mcp/token",
               "GOFORGE_MCP_ALLOW_WRITES=true",
               "$HOME/bin/goforge-mcp"]
    }
  }
}
```

Container on the GoForge network (with project name `goforge` the compose
network is `goforge_goforge_net`):

```json
{
  "mcpServers": {
    "goforge": {
      "command": "ssh",
      "args": ["-T", "minis",
               "docker", "run", "--rm", "-i",
               "--network", "goforge_goforge_net",
               "--env-file", "$HOME/.config/goforge-mcp/env",
               "goforge-mcp:latest"]
    }
  }
}
```

The env file holds `GOFORGE_MCP_TOKEN=…` and, if wanted,
`GOFORGE_MCP_ALLOW_WRITES=true`. Keep it mode `0600`. Do not pass the token
on the command line, where `ps` shows it.

### Compose snippet

For a long-lived definition next to GoForge, add this to an override file.
It is behind a profile, so `docker compose up` does not start it; MCP clients
start it with `docker compose run --rm -T goforge-mcp`.

```yaml
services:
  goforge-mcp:
    build:
      context: .
      target: mcp
    image: goforge-mcp:latest
    profiles: ["mcp"]
    stdin_open: true
    env_file: ${HOME}/.config/goforge-mcp/env
    environment:
      - GOFORGE_MCP_URL=http://goforge:8080
    networks:
      - goforge_net
    # No Docker socket, no database, no published ports.
```

## Tools

Read tools (always on, read-only):

`health`, `whoami`, `dashboard_stats`, `list_projects`, `get_project`,
`list_environments`, `list_env_vars` (secrets masked), `list_deployments`,
`get_deployment`, `get_deployment_logs`, `list_service_templates`,
`get_service_template`, `list_services`, `get_service` (config masked),
`list_containers`, `get_container`, `get_container_logs`,
`get_container_stats`, `list_audit_log`, `list_git_sources`, `list_repos`,
`list_branches`, `get_backup_status` (admin only; see
[Backups](../deployment/backup.md)).

Write tools (only with `GOFORGE_MCP_ALLOW_WRITES=true` and a `write` token):

`trigger_deployment`, `cancel_deployment`, `rollback_deployment`,
`set_env_var`, `delete_env_var`, `sync_env_vars`, `link_service`,
`unlink_service`, `deploy_service`, `restart_service`,
`update_service_image`, `update_service_config`, `restart_container`,
`stop_container`.

Every write tool takes `dry_run` (default `true`) and `approval_ref`:

1. Call it without `dry_run`. It returns the plan and changes nothing.
2. Get a human approval.
3. Call it again with `dry_run: false` and `approval_ref` set to the approval's
   id. GoForge records the `approval_ref` in `audit_logs` before it acts.

`stop_container`, `update_service_config`, and `update_service_image` on a
protected service are refused unless the token has the `protected` scope. The
MCP checks its list first; GoForge checks again and answers
`403 protected_resource` before any Docker call. `restart_service` and
`restart_container` are allowed on protected services.

## Not covered yet

Later versions may let the MCP own Postgres tenants on the shared Postgres,
Redis ACL users on the shared Redis, and backups. v1 has none of these. It
also has no project, service, or volume delete, no prune, and no secret-reveal
tool. The only deleting tools are `delete_env_var` and `unlink_service`, and
they are gated writes like the other 12.
