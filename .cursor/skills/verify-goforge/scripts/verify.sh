#!/usr/bin/env bash
set -euo pipefail

umask 077

PROJECT=goforge-verify
BASE=http://127.0.0.1:8080
STATE_DIR=/tmp/goforge-verify
ROOT=$(git rev-parse --show-toplevel)
EVIDENCE="$ROOT/artifacts/verify-goforge-proof"
BUSY_PORTS=(80 443 5432 8080 8081)

usage() {
  echo "usage: verify.sh launch|doctor|drive-register|psql|check-sealed|cleanup" >&2
  exit 1
}

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing command: $1" >&2
    exit 1
  fi
}

compose() {
  docker compose --env-file "$STATE_DIR/stack.env" -p "$PROJECT" "$@"
}

redact_file() {
  local path="$1"
  [[ -f "$path" ]] || return 0
  python3 - "$path" <<'PY'
import pathlib, re, sys
path = pathlib.Path(sys.argv[1])
text = path.read_text(errors="replace")
text = re.sub(
    r"(?i)(encryption_key|session_secret|csrf_secret|postgres_password|database_url)\s*[:=]\s*\S+",
    r"\1=[redacted]",
    text,
)
text = re.sub(r"postgres://[^@\s]+@", "postgres://[redacted]@", text)
text = re.sub(r"goforge_(session|csrf)=[^;\s\"]+", r"goforge_\1=[redacted]", text)
path.write_text(text)
PY
  chmod 644 "$path"
}

db_psql() {
  local sql="$1"
  compose exec -T db sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -tAc "$1"' sh "$sql"
}

reject_secret_sql() {
  local lower
  lower=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
  case "$lower" in
    *password*|*secret*|*token*|*connection_info*|*config*)
      echo "refusing query that can return secrets" >&2
      exit 1
      ;;
  esac
}

cookie_rows() {
  python3 - "$@" <<'PY'
import pathlib, sys
path, want = sys.argv[1], sys.argv[2] if len(sys.argv) > 2 else ""
found = ""
for raw in pathlib.Path(path).read_text(errors="replace").splitlines():
    line = raw
    if line.startswith("#HttpOnly_"):
        line = line[len("#HttpOnly_"):]
    elif not line or line.startswith("#"):
        continue
    parts = line.split("\t")
    if len(parts) < 7 or not parts[6]:
        continue
    if want and parts[5] != want:
        continue
    found = parts[6]
print(found, end="")
PY
}

csrf_from_jar() {
  cookie_rows "$1" goforge_csrf
}

jar_has_cookie() {
  local jar="$1" name="$2"
  [[ -n "$(cookie_rows "$jar" "$name")" ]]
}

body_has() {
  python3 - "$1" "$2" <<'PY'
import pathlib, sys
text = pathlib.Path(sys.argv[1]).read_text(errors="replace")
print("yes" if sys.argv[2] in text else "no")
PY
}

header_value() {
  python3 - "$1" "$2" <<'PY'
import pathlib, sys
want = sys.argv[2].lower()
value = ""
for line in pathlib.Path(sys.argv[1]).read_text(errors="replace").splitlines():
    if ":" not in line:
        continue
    name, rest = line.split(":", 1)
    if name.strip().lower() == want:
        value = rest.strip()
print(value)
PY
}

port_busy() {
  local port="$1"
  ss -ltnH | awk '{print $4}' | grep -Eq "(^|:)${port}$"
}

write_stack_env() {
  install -d -m 700 "$STATE_DIR"
  local env_file="$STATE_DIR/stack.env"
  cat >"$env_file" <<'EOF'
GOFORGE_DEV_MODE=true
GOFORGE_HOST=0.0.0.0
GOFORGE_PORT=8080
GOFORGE_BASE_URL=http://127.0.0.1:8080
GOFORGE_DOMAIN=localhost
DATABASE_URL=postgres://goforge:password@db:5432/goforge?sslmode=disable
POSTGRES_USER=goforge
POSTGRES_PASSWORD=password
POSTGRES_DB=goforge
ENCRYPTION_KEY=
SESSION_SECRET=
CSRF_SECRET=
DOCKER_HOST=unix:///var/run/docker.sock
TRAEFIK_NETWORK=traefik
ACME_EMAIL=admin@example.com
TRAEFIK_DASHBOARD_PORT=8081
TRAEFIK_DASHBOARD_INSECURE=false
TRAEFIK_LOG_LEVEL=INFO
TEMPLATES_DIR=./templates
LOG_LEVEL=info
LOG_FORMAT=json
EOF
  chmod 600 "$env_file"
}

refuse() {
  local reason="$1"
  install -d -m 755 "$EVIDENCE"
  printf '%s\n' "$reason" | tee "$EVIDENCE/BLOCKED.txt" >&2
  chmod 644 "$EVIDENCE/BLOCKED.txt"
  exit 1
}

cmd_launch() {
  need_cmd docker
  need_cmd python3
  need_cmd curl
  need_cmd ss
  install -d -m 755 "$EVIDENCE"
  git -C "$ROOT" rev-parse HEAD >"$EVIDENCE/revision.txt"
  chmod 644 "$EVIDENCE/revision.txt"

  if [[ -f "$ROOT/.env" ]]; then
    refuse "refusing launch: $ROOT/.env exists. This run does not read or modify it."
  fi
  if ! docker info >/dev/null 2>&1; then
    refuse "refusing launch: docker info failed."
  fi
  if ! docker compose version >/dev/null 2>&1; then
    refuse "refusing launch: docker compose is not available."
  fi
  if docker compose -p "$PROJECT" ps -q 2>/dev/null | grep -q .; then
    refuse "refusing launch: project $PROJECT already has containers."
  fi
  local port
  for port in "${BUSY_PORTS[@]}"; do
    if port_busy "$port"; then
      refuse "refusing launch: port $port is already listening. This compose file publishes fixed ports, so only one stack can run."
    fi
  done
  if docker network inspect traefik >/dev/null 2>&1; then
    refuse "refusing launch: docker network traefik already exists."
  fi

  write_stack_env
  if ! compose up -d --build 2>&1 | tee "$EVIDENCE/launch.log"; then
    fail_launch "compose up failed"
  fi
  redact_file "$EVIDENCE/launch.log"

  local attempt code
  for attempt in $(seq 1 120); do
    code=$(curl -sS -o "$STATE_DIR/ready.json" -w '%{http_code}' "$BASE/health/ready" || true)
    if [[ "$code" == "200" ]] && python3 - "$STATE_DIR/ready.json" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    sys.exit(1)
raise SystemExit(0 if data.get("checks", {}).get("database") == "ok" else 1)
PY
    then
      cp "$STATE_DIR/ready.json" "$EVIDENCE/ready.json"
      chmod 644 "$EVIDENCE/ready.json"
      echo "ready: database ok after ${attempt} checks"
      return 0
    fi
    sleep 5
  done
  if [[ -f "$STATE_DIR/ready.json" ]]; then
    cp "$STATE_DIR/ready.json" "$EVIDENCE/ready.json"
    redact_file "$EVIDENCE/ready.json"
  fi
  fail_launch "timed out waiting for /health/ready database ok"
}

fail_launch() {
  local msg="$1"
  if [[ -f "$EVIDENCE/launch.log" ]]; then
    redact_file "$EVIDENCE/launch.log"
  fi
  if [[ -f "$STATE_DIR/stack.env" ]]; then
    compose down -v --remove-orphans >>"$EVIDENCE/launch.log" 2>&1 || true
    redact_file "$EVIDENCE/launch.log"
  fi
  echo "$msg" >&2
  exit 1
}

cmd_doctor() {
  need_cmd curl
  need_cmd python3
  install -d -m 755 "$EVIDENCE"
  local report="$EVIDENCE/doctor.txt"
  local container="missing" health_code="000" ready_code="000"
  local database="missing" docker_check="missing" app_port="missing" db_port="missing"
  local drive_auth=no drive_docker=no

  if [[ -f "$STATE_DIR/stack.env" ]]; then
    container=$(compose ps -q goforge || true)
    app_port=$(compose port goforge 8080 2>/dev/null || true)
    db_port=$(compose port db 5432 2>/dev/null || true)
  fi
  if [[ -n "$container" && "$container" != "missing" ]]; then
    container="running"
  else
    container="missing"
  fi

  health_code=$(curl -sS -o "$STATE_DIR/health.json" -w '%{http_code}' "$BASE/health" || true)
  ready_code=$(curl -sS -o "$STATE_DIR/ready.json" -w '%{http_code}' "$BASE/health/ready" || true)
  if [[ -f "$STATE_DIR/ready.json" ]]; then
    database=$(python3 - "$STATE_DIR/ready.json" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    print("unparsed")
    raise SystemExit
print(data.get("checks", {}).get("database", "missing"))
PY
)
    docker_check=$(python3 - "$STATE_DIR/ready.json" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    print("unparsed")
    raise SystemExit
print(data.get("checks", {}).get("docker", "missing"))
PY
)
    cp "$STATE_DIR/ready.json" "$EVIDENCE/ready.json"
    redact_file "$EVIDENCE/ready.json"
  fi

  if [[ "$container" == "running" && "$health_code" == "200" && "$database" == "ok" ]]; then
    drive_auth=yes
  fi
  if [[ "$drive_auth" == "yes" && "$docker_check" == "ok" ]]; then
    drive_docker=yes
  fi

  cat >"$report" <<EOF
project=$PROJECT
container=$container
health_code=$health_code
ready_code=$ready_code
database=$database
docker=$docker_check
app_port=${app_port:-missing}
db_port=${db_port:-missing}
drive_auth=$drive_auth
drive_docker=$drive_docker
EOF
  chmod 644 "$report"
  cat "$report"
  if [[ "$drive_auth" != "yes" ]]; then
    exit 1
  fi
}

cmd_drive_register() {
  need_cmd curl
  need_cmd python3
  need_cmd openssl
  install -d -m 755 "$EVIDENCE"
  local jar="$STATE_DIR/jar-a.txt"
  local page="$STATE_DIR/register.html"
  local headers="$STATE_DIR/register.headers"
  local dash="$STATE_DIR/dashboard.html"
  local again="$STATE_DIR/register-again.html"
  local verdict="$EVIDENCE/register-verdict.txt"
  local email="verify-admin@example.test"
  local open=fail submit=fail closed=fail harness=ok
  local page_status post_status location dash_status again_status users_count heading again_heading session_cookie

  users_count=$(db_psql "select count(*) from users;" | tr -d '[:space:]')
  if [[ "$users_count" != "0" ]]; then
    cat >"$verdict" <<EOF
harness=fail
reason=users table is not empty
users-count=$users_count
EOF
    chmod 644 "$verdict"
    exit 2
  fi

  page_status=$(curl -sS -c "$jar" -D "$headers" -o "$page" -w '%{http_code}' "$BASE/auth/register" || true)
  local open_heading
  open_heading=$(body_has "$page" "Create your account")
  if [[ "$page_status" == "200" && "$open_heading" == "yes" ]]; then
    open=pass
  fi
  local csrf
  csrf=$(csrf_from_jar "$jar")
  if [[ -z "$csrf" ]]; then
    cat >"$verdict" <<EOF
harness=fail
reason=register page did not set goforge_csrf
register-page-status=$page_status
EOF
    chmod 644 "$verdict"
    rm -f "$page" "$headers"
    exit 2
  fi

  openssl rand -hex 16 >"$STATE_DIR/register.password"
  chmod 600 "$STATE_DIR/register.password"
  post_status=$(curl -sS -c "$jar" -b "$jar" -D "$headers" -o "$page" -w '%{http_code}' \
    -X POST "$BASE/auth/register" \
    --data-urlencode "csrf_token=$csrf" \
    --data-urlencode "email=$email" \
    --data-urlencode "password@$STATE_DIR/register.password" \
    --data-urlencode "confirm_password@$STATE_DIR/register.password" || true)
  location=$(header_value "$headers" "Location")
  case "$location" in
    */dashboard) location="/dashboard" ;;
  esac
  if jar_has_cookie "$jar" "goforge_session"; then
    session_cookie=present
  else
    session_cookie=missing
  fi

  dash_status=$(curl -sS -b "$jar" -c "$jar" -o "$dash" -w '%{http_code}' "$BASE/dashboard" || true)
  heading=$(body_has "$dash" "Dashboard")
  users_count=$(db_psql "select count(*) from users;" | tr -d '[:space:]')
  if [[ "$post_status" == "303" && "$location" == "/dashboard" && "$session_cookie" == "present" && "$dash_status" == "200" && "$heading" == "yes" && "$users_count" == "1" ]]; then
    submit=pass
  else
    harness=fail
  fi

  csrf=$(csrf_from_jar "$jar")
  again_status=$(curl -sS -b "$jar" -c "$jar" -o "$again" -w '%{http_code}' "$BASE/auth/register" || true)
  again_heading=$(body_has "$again" "Create your account")
  if [[ "$again_status" == "403" ]]; then
    closed=pass
  fi

  cat >"$verdict" <<EOF
harness=$harness
register-open=$open
register-submit=$submit
register-closed=$closed
register-page-status=$page_status
register-post-status=$post_status
register-post-location=$location
session-cookie=$session_cookie
dashboard-status=$dash_status
dashboard-heading=$heading
users-count=$users_count
register-again-status=$again_status
register-again-heading=$again_heading
fixture-email=$email
EOF
  if [[ "$closed" != "pass" ]]; then
    cat >>"$verdict" <<'EOF'
register-closed-reason=GET /auth/register stayed open after the first user. This tree checks GOFORGE_ENABLE_REGISTRATION, which defaults to true, and does not close registration from the users count.
EOF
  fi
  chmod 644 "$verdict"
  rm -f "$page" "$dash" "$again" "$headers"
  cat "$verdict"
  if [[ "$harness" != "ok" ]]; then
    exit 2
  fi
}

cmd_psql() {
  local sql="${1:-}"
  if [[ -z "$sql" ]]; then
    echo "usage: verify.sh psql \"SQL\"" >&2
    exit 1
  fi
  reject_secret_sql "$sql"
  db_psql "$sql"
}

cmd_check_sealed() {
  local name="${1:-}"
  if [[ ! "$name" =~ ^[a-z0-9][a-z0-9-]{0,62}$ ]]; then
    echo "usage: verify.sh check-sealed <service-name>" >&2
    exit 1
  fi
  if [[ ! -f "$STATE_DIR/service.password" ]]; then
    echo "missing $STATE_DIR/service.password" >&2
    exit 1
  fi
  local row="$STATE_DIR/service-row.txt"
  install -m 600 /dev/null "$row"
  db_psql "select coalesce(string_agg(config::text || E'\n' || connection_info::text, E'\n'), '') from service_instances where name = '$name'" >"$row" || true
  local rc=0
  set +e
  python3 - "$row" "$STATE_DIR/service.password" <<'PY'
import pathlib, sys
row = pathlib.Path(sys.argv[1]).read_text(errors="replace").strip()
password = pathlib.Path(sys.argv[2]).read_text(errors="replace").strip()
if not row:
    print("sealed_marker=no")
    print("plaintext_password_present=no")
    print("row=missing")
    raise SystemExit(2)
print("sealed_marker=" + ("yes" if "_goforge_enc" in row else "no"))
print("plaintext_password_present=" + ("yes" if password and password in row else "no"))
print("row=present")
PY
  rc=$?
  set -e
  rm -f "$row"
  return "$rc"
}

evidence_list() {
  find "$EVIDENCE" -type f | sort
}

cmd_cleanup() {
  install -d -m 755 "$EVIDENCE"
  local before="$STATE_DIR/evidence-before.txt"
  install -d -m 700 "$STATE_DIR"
  evidence_list >"$before" || true
  if [[ -f "$STATE_DIR/stack.env" ]]; then
    compose down -v --remove-orphans >"$EVIDENCE/cleanup.log" 2>&1 || true
  else
    docker compose -p "$PROJECT" down -v --remove-orphans >"$EVIDENCE/cleanup.log" 2>&1 || true
  fi
  redact_file "$EVIDENCE/cleanup.log"
  if docker network inspect traefik >/dev/null 2>&1; then
    local attached
    attached=$(docker network inspect traefik -f '{{len .Containers}}' 2>/dev/null || echo 1)
    if [[ "$attached" == "0" ]]; then
      docker network rm traefik >>"$EVIDENCE/cleanup.log" 2>&1 || true
    fi
  fi
  redact_file "$EVIDENCE/cleanup.log"
  local missing=0 file
  while IFS= read -r file; do
    [[ -z "$file" ]] && continue
    if [[ ! -f "$file" ]]; then
      echo "evidence missing after cleanup: $file" >&2
      missing=1
    fi
  done <"$before"
  rm -rf "$STATE_DIR"
  if [[ "$missing" -ne 0 ]]; then
    exit 1
  fi
  echo "cleanup done. evidence remains in $EVIDENCE"
}

main() {
  local cmd="${1:-}"
  case "$cmd" in
    launch) cmd_launch ;;
    doctor) cmd_doctor ;;
    drive-register) cmd_drive_register ;;
    psql) shift; cmd_psql "${1:-}" ;;
    check-sealed) shift; cmd_check_sealed "${1:-}" ;;
    cleanup) cmd_cleanup ;;
    *) usage ;;
  esac
}

main "$@"
