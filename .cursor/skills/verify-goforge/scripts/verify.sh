#!/usr/bin/env bash
set -euo pipefail

umask 077

PROJECT=goforge-verify
# GOFORGE_VERIFY_BASE, GOFORGE_VERIFY_PSQL, and GOFORGE_VERIFY_APP_LOGS point
# the drives at a stack verify.sh did not launch (see features/backups.md).
BASE="${GOFORGE_VERIFY_BASE:-http://127.0.0.1:8080}"
VERIFY_DIR=/tmp/goforge-verify
STATE_DIR="$VERIFY_DIR/state"
ROOT=$(git rev-parse --show-toplevel)
# Evidence lives outside the repo so proofs are never committed.
EVIDENCE="${GOFORGE_VERIFY_PROOF:-$VERIFY_DIR/proof}"
# Canonicalize before the inside-repo check so relative and /tmp/../repo paths cannot bypass it.
EVIDENCE=$(realpath -m "$EVIDENCE")
# Compose publishes the app on 127.0.0.1:8080, Traefik on 80 and 443, and the
# (default-off) Traefik dashboard on 127.0.0.1:8081.
# Postgres is not published on the host.
BUSY_PORTS=(80 443 8080 8081)
# Set GOFORGE_VERIFY_DOCKER="sudo -n docker" when the user is not in the docker group.
read -r -a DOCKER <<<"${GOFORGE_VERIFY_DOCKER:-docker}"
ADMIN_EMAIL="${GOFORGE_VERIFY_ADMIN_EMAIL:-verify-admin@example.test}"
# Registered with different case to prove GOFORGE_ADMIN_EMAIL matching ignores case.
REGISTER_EMAIL="${GOFORGE_VERIFY_REGISTER_EMAIL:-Verify-Admin@Example.Test}"
ENABLE_REGISTRATION="${GOFORGE_VERIFY_ENABLE_REGISTRATION:-false}"

case "$EVIDENCE" in
  "$ROOT"|"$ROOT"/*)
    echo "refusing: evidence directory $EVIDENCE is inside the repo. Keep proofs under $VERIFY_DIR/proof." >&2
    exit 1
    ;;
esac

usage() {
  echo "usage: verify.sh launch|doctor|drive-register|drive-login|drive-backup-settings|drive-mcp-http|drive-ui-a11y|check-backup-sealed|psql|check-sealed|cleanup" >&2
  exit 1
}

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing command: $1" >&2
    exit 1
  fi
}

compose() {
  "${DOCKER[@]}" compose -f "$ROOT/docker-compose.yml" --project-directory "$ROOT" --env-file "$STATE_DIR/stack.env" -p "$PROJECT" "$@"
}

redact_file() {
  local path="$1"
  [[ -f "$path" ]] || return 0
  python3 - "$path" <<'PY'
import pathlib, re, sys
path = pathlib.Path(sys.argv[1])
text = path.read_text(errors="replace")
text = re.sub(
    r"(?i)(encryption_key|session_secret|csrf_secret|webhook_secret|postgres_password|database_url)\s*[:=]\s*\S+",
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
  if [[ -n "${GOFORGE_VERIFY_PSQL:-}" ]]; then
    # A command that runs psql -tA with -c appended, e.g.
    # "sudo -n docker exec -i gfbk-test-pg psql -U goforge -d goforge -v ON_ERROR_STOP=1 -tA".
    local -a cmd
    read -r -a cmd <<<"$GOFORGE_VERIFY_PSQL"
    "${cmd[@]}" -c "$sql"
    return
  fi
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
  local pg_password
  pg_password=$(openssl rand -hex 24)
  cat >"$env_file" <<EOF
GOFORGE_DEV_MODE=false
GOFORGE_HOST=0.0.0.0
GOFORGE_PORT=8080
GOFORGE_BASE_URL=http://127.0.0.1:8080
GOFORGE_DOMAIN=localhost
DATABASE_URL=postgres://goforge:${pg_password}@db:5432/goforge?sslmode=disable
POSTGRES_USER=goforge
POSTGRES_PASSWORD=${pg_password}
POSTGRES_DB=goforge
ENCRYPTION_KEY=$(openssl rand -base64 32)
SESSION_SECRET=$(openssl rand -base64 32)
CSRF_SECRET=$(openssl rand -base64 32)
WEBHOOK_SECRET=$(openssl rand -base64 32)
GOFORGE_ENABLE_REGISTRATION=${ENABLE_REGISTRATION}
GOFORGE_ADMIN_EMAIL=${ADMIN_EMAIL}
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
  need_cmd openssl
  install -d -m 755 "$EVIDENCE"
  git -C "$ROOT" rev-parse HEAD >"$EVIDENCE/revision.txt"
  chmod 644 "$EVIDENCE/revision.txt"

  if [[ -f "$ROOT/.env" ]]; then
    refuse "refusing launch: $ROOT/.env exists. This run does not read or modify it."
  fi
  if ! "${DOCKER[@]}" info >/dev/null 2>&1; then
    refuse "refusing launch: ${DOCKER[*]} info failed. Set GOFORGE_VERIFY_DOCKER=\"sudo -n docker\" if the socket needs root."
  fi
  if ! "${DOCKER[@]}" compose version >/dev/null 2>&1; then
    refuse "refusing launch: docker compose is not available."
  fi
  if "${DOCKER[@]}" compose -p "$PROJECT" ps -q 2>/dev/null | grep -q .; then
    refuse "refusing launch: project $PROJECT already has containers."
  fi
  local port
  for port in "${BUSY_PORTS[@]}"; do
    if port_busy "$port"; then
      refuse "refusing launch: port $port is already listening. This compose file publishes fixed ports, so only one stack can run."
    fi
  done
  if "${DOCKER[@]}" network inspect traefik >/dev/null 2>&1; then
    refuse "refusing launch: docker network traefik already exists."
  fi

  write_stack_env
  rm -f "$EVIDENCE/BLOCKED.txt"
  if ! (cd "$ROOT" && compose up -d --build) 2>&1 | tee "$EVIDENCE/launch.log"; then
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
  local app_loopback=no drive_auth=no drive_docker=no

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

  case "${app_port:-}" in
    127.0.0.1:*) app_loopback=yes ;;
  esac
  case "${db_port:-}" in
    ""|":0"|"missing") db_port="unpublished" ;;
  esac

  if [[ "$container" == "running" && "$health_code" == "200" && "$database" == "ok" && "$app_loopback" == "yes" ]]; then
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
app_loopback=$app_loopback
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
  local email="$REGISTER_EMAIL"
  local open=fail submit=fail closed=fail admin=fail closed_post=fail harness=ok
  local role stored_email post_again_status
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

  # A second account POST from a fresh jar must also be refused.
  local jar_b="$STATE_DIR/jar-other.txt"
  curl -sS -c "$jar_b" -o /dev/null "$BASE/login" || true
  local csrf_b
  csrf_b=$(csrf_from_jar "$jar_b")
  post_again_status=$(curl -sS -c "$jar_b" -b "$jar_b" -o /dev/null -w '%{http_code}' \
    -X POST "$BASE/auth/register" \
    --data-urlencode "csrf_token=$csrf_b" \
    --data-urlencode "email=verify-other@example.test" \
    --data-urlencode "password@$STATE_DIR/register.password" \
    --data-urlencode "confirm_password@$STATE_DIR/register.password" || true)
  if [[ "$post_again_status" == "403" ]]; then
    closed_post=pass
  fi
  rm -f "$jar_b"

  stored_email=$(db_psql "select email from users;" | tr -d '[:space:]')
  role=$(db_psql "select role from users;" | tr -d '[:space:]')
  if [[ "$role" == "admin" ]]; then
    admin=pass
  fi

  cat >"$verdict" <<EOF
harness=$harness
register-open=$open
register-submit=$submit
register-closed=$closed
register-closed-post=$closed_post
register-admin-role=$admin
register-page-status=$page_status
register-post-status=$post_status
register-post-location=$location
session-cookie=$session_cookie
dashboard-status=$dash_status
dashboard-heading=$heading
users-count=$users_count
register-again-status=$again_status
register-again-heading=$again_heading
register-other-post-status=$post_again_status
users-count-after=$(db_psql "select count(*) from users;" | tr -d '[:space:]')
fixture-email=$email
admin-email-env=$ADMIN_EMAIL
stored-email=$stored_email
stored-role=$role
EOF
  if [[ "$closed" != "pass" || "$closed_post" != "pass" ]]; then
    echo "register-closed-reason=registration stayed open after the first user with GOFORGE_ENABLE_REGISTRATION=$ENABLE_REGISTRATION" >>"$verdict"
  fi
  chmod 644 "$verdict"
  rm -f "$page" "$dash" "$again" "$headers"
  cat "$verdict"
  if [[ "$harness" != "ok" ]]; then
    exit 2
  fi
}

cmd_drive_login() {
  need_cmd curl
  need_cmd python3
  install -d -m 755 "$EVIDENCE"
  if [[ ! -f "$STATE_DIR/register.password" ]]; then
    echo "missing $STATE_DIR/register.password. Run drive-register first." >&2
    exit 1
  fi
  local jar="$STATE_DIR/jar-login.txt"
  local headers="$STATE_DIR/login.headers"
  local page="$STATE_DIR/login.html"
  local verdict="$EVIDENCE/login-verdict.txt"
  local email
  email=$(printf '%s' "$ADMIN_EMAIL" | tr '[:upper:]' '[:lower:]')
  local login=fail list=fail create=fail listed=fail harness=ok
  local login_page_status login_status location dash_status list_status list_heading create_status create_location detail_status listed_name

  rm -f "$jar"
  login_page_status=$(curl -sS -c "$jar" -o "$page" -w '%{http_code}' "$BASE/login" || true)
  local csrf
  csrf=$(csrf_from_jar "$jar")
  login_status=$(curl -sS -c "$jar" -b "$jar" -D "$headers" -o /dev/null -w '%{http_code}' \
    -X POST "$BASE/auth/login" \
    --data-urlencode "csrf_token=$csrf" \
    --data-urlencode "email=$email" \
    --data-urlencode "password@$STATE_DIR/register.password" || true)
  location=$(header_value "$headers" "Location")
  dash_status=$(curl -sS -b "$jar" -c "$jar" -o /dev/null -w '%{http_code}' "$BASE/dashboard" || true)
  if [[ "$login_status" == "303" && "$location" == */dashboard && "$dash_status" == "200" ]]; then
    login=pass
  fi

  list_status=$(curl -sS -b "$jar" -c "$jar" -o "$page" -w '%{http_code}' "$BASE/projects" || true)
  list_heading=$(body_has "$page" "View and manage your application projects.")
  if [[ "$list_status" == "200" && "$list_heading" == "yes" ]]; then
    list=pass
  fi

  csrf=$(csrf_from_jar "$jar")
  create_status=$(curl -sS -b "$jar" -c "$jar" -D "$headers" -o /dev/null -w '%{http_code}' \
    -X POST "$BASE/projects" \
    --data-urlencode "csrf_token=$csrf" \
    --data-urlencode "name=verify-smoke" \
    --data-urlencode "repository_url=https://github.com/raythurman2386/goforge.git" \
    --data-urlencode "default_branch=main" \
    --data-urlencode "dockerfile_path=Dockerfile" \
    --data-urlencode "build_context=." || true)
  create_location=$(header_value "$headers" "Location")
  create_location=$(printf '%s' "$create_location" | sed -E 's#^https?://[^/]+##')
  if [[ "$create_status" == "303" && "$create_location" == /projects/* ]]; then
    create=pass
    detail_status=$(curl -sS -b "$jar" -c "$jar" -o /dev/null -w '%{http_code}' "$BASE$create_location" || true)
  fi
  curl -sS -b "$jar" -c "$jar" -o "$page" "$BASE/projects" >/dev/null || true
  listed_name=$(body_has "$page" "verify-smoke")
  if [[ "$listed_name" == "yes" ]]; then
    listed=pass
  fi
  [[ "$login" == "pass" ]] || harness=fail

  cat >"$verdict" <<EOF
harness=$harness
login=$login
projects-list=$list
project-create=$create
project-listed=$listed
login-page-status=$login_page_status
login-post-status=$login_status
login-post-location=$(printf '%s' "$location" | sed -E 's#^https?://[^/]+##')
dashboard-status=$dash_status
projects-status=$list_status
projects-heading=$list_heading
project-create-status=$create_status
project-create-location=${create_location:-missing}
project-detail-status=${detail_status:-missing}
project-db-row=$(db_psql "select count(*) from projects where name = 'verify-smoke';" | tr -d '[:space:]')
login-email=$email
EOF
  chmod 644 "$verdict"
  rm -f "$page" "$headers"
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

# ---- Backup settings (Settings → Profile) ----------------------------------
# A throwaway MinIO the app can reach. Keys live in $STATE_DIR (mode 600) and
# are never printed.
BK_MINIO=gfbk-verify-minio
BK_BUCKET=gfbk-verify
BK_IMAGE="${GOFORGE_VERIFY_MINIO_IMAGE:-cgr.dev/chainguard/minio:latest}"

app_logs() {
  if [[ -n "${GOFORGE_VERIFY_APP_LOGS:-}" ]]; then
    local -a cmd
    read -r -a cmd <<<"$GOFORGE_VERIFY_APP_LOGS"
    "${cmd[@]}" 2>&1
    return
  fi
  compose logs --no-color goforge 2>&1
}

# check_backup_sealed prints booleans only: both sealed columns are set and
# neither holds the plaintext key.
cmd_check_backup_sealed() {
  local row="$STATE_DIR/backup-row.txt"
  if [[ ! -f "$STATE_DIR/minio.user" ]]; then
    echo "missing $STATE_DIR/minio.user. Run drive-backup-settings first." >&2
    exit 1
  fi
  install -m 600 /dev/null "$row"
  db_psql "select access_key_id_sealed || E'\n' || secret_access_key_sealed from backup_settings" >"$row" || true
  local rc=0
  set +e
  python3 - "$row" "$STATE_DIR/minio.user" "$STATE_DIR/minio.password" <<'PY'
import pathlib, sys
lines = pathlib.Path(sys.argv[1]).read_text(errors="replace").strip().splitlines()
ak = pathlib.Path(sys.argv[2]).read_text().strip()
sk = pathlib.Path(sys.argv[3]).read_text().strip()
if len(lines) != 2:
    print("row=missing")
    raise SystemExit(2)
text = "\n".join(lines)
print("row=present")
print("access_key_sealed=" + ("yes" if lines[0] and lines[0] != ak else "no"))
print("secret_sealed=" + ("yes" if lines[1] and lines[1] != sk else "no"))
print("plaintext_access_key_present=" + ("yes" if ak in text else "no"))
print("plaintext_secret_present=" + ("yes" if sk in text else "no"))
raise SystemExit(0 if lines[0] != ak and lines[1] != sk and ak not in text and sk not in text else 2)
PY
  rc=$?
  set -e
  rm -f "$row"
  return "$rc"
}

bk_minio_up() {
  "${DOCKER[@]}" rm -f "$BK_MINIO" >/dev/null 2>&1 || true
  openssl rand -hex 10 >"$STATE_DIR/minio.user"
  openssl rand -hex 20 >"$STATE_DIR/minio.password"
  chmod 600 "$STATE_DIR/minio.user" "$STATE_DIR/minio.password"
  local -a net=()
  if [[ -z "${GOFORGE_VERIFY_BASE:-}" ]]; then
    net=(--network "${PROJECT}_default")
  fi
  # An env file, not -e or a pipe: sudo drops both, and argv is visible in ps.
  local envf="$STATE_DIR/minio.env"
  install -m 600 /dev/null "$envf"
  printf 'MINIO_ROOT_USER=%s\nMINIO_ROOT_PASSWORD=%s\n' "$(cat "$STATE_DIR/minio.user")" "$(cat "$STATE_DIR/minio.password")" >"$envf"
  "${DOCKER[@]}" run -d --name "$BK_MINIO" "${net[@]}" -p 127.0.0.1::9000 --env-file "$envf" \
    "$BK_IMAGE" server /data >/dev/null
  rm -f "$envf"
  local hostport
  hostport=$("${DOCKER[@]}" port "$BK_MINIO" 9000 | head -1)
  printf '%s' "${hostport##*:}" >"$STATE_DIR/minio.port"
  local i
  for i in $(seq 1 30); do
    if curl -sS -o /dev/null "http://127.0.0.1:$(cat "$STATE_DIR/minio.port")/minio/health/live" 2>/dev/null; then
      break
    fi
    sleep 1
  done
  local netrc="$STATE_DIR/minio.netrc"
  install -m 600 /dev/null "$netrc"
  printf 'machine 127.0.0.1 login %s password %s\n' "$(cat "$STATE_DIR/minio.user")" "$(cat "$STATE_DIR/minio.password")" >"$netrc"
  curl -sS -o /dev/null -w '%{http_code}' --aws-sigv4 "aws:amz:us-east-1:s3" --netrc-file "$netrc" \
    -X PUT "http://127.0.0.1:$(cat "$STATE_DIR/minio.port")/$BK_BUCKET" || true
  rm -f "$netrc"
}

# bk_endpoint is the MinIO URL as the app sees it.
bk_endpoint() {
  if [[ -n "${GOFORGE_VERIFY_BASE:-}" ]]; then
    printf 'http://127.0.0.1:%s' "$(cat "$STATE_DIR/minio.port")"
  else
    printf 'http://%s:9000' "$BK_MINIO"
  fi
}

# bk_login signs in as email with the password file into jar.
bk_login() {
  local jar="$1" email="$2" pwfile="$3"
  rm -f "$jar"
  curl -sS -c "$jar" -o /dev/null "$BASE/login" || true
  curl -sS -c "$jar" -b "$jar" -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/login" \
    --data-urlencode "csrf_token=$(csrf_from_jar "$jar")" \
    --data-urlencode "email=$email" \
    --data-urlencode "password@$pwfile" || true
}

# bk_send METHOD PATH JAR OUT [curl args...] sends an HTMX form request with
# the jar's CSRF token and prints the status.
bk_send() {
  local method="$1" path="$2" jar="$3" out="$4"
  shift 4
  curl -sS -b "$jar" -c "$jar" -o "$out" -w '%{http_code}' -X "$method" "$BASE$path" \
    -H "HX-Request: true" -H "X-CSRF-Token: $(csrf_from_jar "$jar")" "$@" || true
}

# bk_leaks FILE prints yes when FILE holds a MinIO key or the wrong secret.
bk_leaks() {
  python3 - "$1" "$STATE_DIR/minio.user" "$STATE_DIR/minio.password" "$STATE_DIR/wrong.secret" <<'PY'
import pathlib, sys
text = pathlib.Path(sys.argv[1]).read_text(errors="replace") if pathlib.Path(sys.argv[1]).exists() else ""
vals = [pathlib.Path(p).read_text().strip() for p in sys.argv[2:] if pathlib.Path(p).exists()]
print("yes" if any(v and v in text for v in vals) else "no")
PY
}

bk_count_log() {
  app_logs | grep -c -- "$1" || true
}

cmd_drive_backup_settings() {
  need_cmd curl
  need_cmd python3
  need_cmd openssl
  install -d -m 755 "$EVIDENCE"
  install -d -m 700 "$STATE_DIR"
  if [[ ! -f "$STATE_DIR/register.password" ]]; then
    echo "missing $STATE_DIR/register.password. Run drive-register first." >&2
    exit 1
  fi
  local verdict="$EVIDENCE/backup-settings-verdict.txt"
  local jar="$STATE_DIR/jar-backup.txt" page="$STATE_DIR/bk-page.html" out="$STATE_DIR/bk-out.html" hdr="$STATE_DIR/bk.headers"
  local admin_email
  admin_email=$(printf '%s' "$ADMIN_EMAIL" | tr '[:upper:]' '[:lower:]')
  local harness=ok edit=fail masked=fail sealed=fail live=fail blank=fail csrf=fail nonadmin=skipped test_ok=fail test_bad=fail disable=fail audit=fail

  local login_status bucket_status
  login_status=$(bk_login "$jar" "$admin_email" "$STATE_DIR/register.password")
  [[ "$login_status" == "303" ]] || harness=fail
  bucket_status=$(bk_minio_up)
  [[ "$bucket_status" == "200" ]] || harness=fail
  openssl rand -hex 16 | sed 's/^/wrong-/' >"$STATE_DIR/wrong.secret"
  chmod 600 "$STATE_DIR/wrong.secret"

  # Fresh stack: no BACKUP_* env, so the card is off and the form is empty.
  local profile_status cache_control form_present off_before
  profile_status=$(curl -sS -b "$jar" -c "$jar" -D "$hdr" -o "$page" -w '%{http_code}' "$BASE/settings/profile" || true)
  cache_control=$(header_value "$hdr" "Cache-Control")
  form_present=$(body_has "$page" 'data-testid="backup-settings-form"')
  off_before=$(body_has "$page" "Backups are off")

  # backup-settings-admin-edit + live-apply: save, then the card shows the
  # new schedule and the scheduler logs it without a restart.
  local sched_before save_status save_toast save_leak
  sched_before=$(bk_count_log "next backup scheduled")
  save_status=$(bk_send PUT /settings/backups "$jar" "$out" \
    --data-urlencode "enabled=true" --data-urlencode "endpoint=$(bk_endpoint)" \
    --data-urlencode "region=us-east-1" --data-urlencode "bucket=$BK_BUCKET" --data-urlencode "prefix=verify/" \
    --data-urlencode "access_key_id@$STATE_DIR/minio.user" --data-urlencode "secret_access_key@$STATE_DIR/minio.password" \
    --data-urlencode "force_path_style=true" --data-urlencode "schedule_time=03:30" --data-urlencode "schedule_zone=UTC" \
    --data-urlencode "retention_daily=7" --data-urlencode "timeout=2h")
  save_toast=$(body_has "$out" "Backup settings saved")
  save_leak=$(bk_leaks "$out")
  if [[ "$save_status" == "200" && "$save_toast" == "yes" && "$save_leak" == "no" ]]; then
    edit=pass
  fi
  sleep 2
  local sched_after card_schedule
  sched_after=$(bk_count_log "next backup scheduled")
  curl -sS -b "$jar" -c "$jar" -o "$page" "$BASE/settings/profile" >/dev/null || true
  card_schedule=$(body_has "$page" "Schedule 03:30 UTC, keeping 7 days, to $BK_BUCKET/verify/.")
  if [[ "$card_schedule" == "yes" && "$sched_after" -gt "$sched_before" ]]; then
    live=pass
  fi

  # backup-settings-secret-masked
  local page_leak hint_shown secret_state ak_last4
  ak_last4=$(tr -d "\n" <"$STATE_DIR/minio.user" | tail -c 4)
  page_leak=$(bk_leaks "$page")
  hint_shown=$(body_has "$page" "ending in $ak_last4")
  secret_state=$(body_has "$page" "Set. Leave blank to keep it; type a new one to rotate it.")
  if [[ "$page_leak" == "no" && "$hint_shown" == "yes" && "$secret_state" == "yes" ]]; then
    masked=pass
  fi
  local sealed_out
  sealed_out=$(cmd_check_backup_sealed 2>&1 || true)
  if grep -q "access_key_sealed=yes" <<<"$sealed_out" && grep -q "secret_sealed=yes" <<<"$sealed_out" &&
     grep -q "plaintext_secret_present=no" <<<"$sealed_out" && grep -q "plaintext_access_key_present=no" <<<"$sealed_out"; then
    sealed=pass
  fi

  # backup-settings-test-connection: blank keys test the stored ones.
  local test_status test_body bad_status bad_leak
  test_status=$(bk_send POST /settings/backups/test "$jar" "$out" \
    --data-urlencode "endpoint=$(bk_endpoint)" --data-urlencode "region=us-east-1" --data-urlencode "bucket=$BK_BUCKET" \
    --data-urlencode "prefix=verify/" --data-urlencode "force_path_style=true" --data-urlencode "schedule_time=03:30" \
    --data-urlencode "schedule_zone=UTC" --data-urlencode "retention_daily=7" --data-urlencode "timeout=2h")
  test_body=$(body_has "$out" "Connected.")
  [[ "$test_status" == "200" && "$test_body" == "yes" ]] && test_ok=pass
  bad_status=$(bk_send POST /settings/backups/test "$jar" "$out" \
    --data-urlencode "endpoint=$(bk_endpoint)" --data-urlencode "region=us-east-1" --data-urlencode "bucket=$BK_BUCKET" \
    --data-urlencode "prefix=verify/" --data-urlencode "force_path_style=true" --data-urlencode "secret_access_key@$STATE_DIR/wrong.secret" \
    --data-urlencode "schedule_time=03:30" --data-urlencode "schedule_zone=UTC" --data-urlencode "retention_daily=7" --data-urlencode "timeout=2h")
  bad_leak=$(bk_leaks "$out")
  [[ "$bad_status" == "502" && "$bad_leak" == "no" ]] && test_bad=pass

  # backup-settings-blank-keeps: save with blank keys, then test again.
  local blank_status blank_test retention_after
  blank_status=$(bk_send PUT /settings/backups "$jar" "$out" \
    --data-urlencode "enabled=true" --data-urlencode "endpoint=$(bk_endpoint)" --data-urlencode "region=us-east-1" \
    --data-urlencode "bucket=$BK_BUCKET" --data-urlencode "prefix=verify/" --data-urlencode "force_path_style=true" \
    --data-urlencode "schedule_time=03:30" --data-urlencode "schedule_zone=UTC" --data-urlencode "retention_daily=5" --data-urlencode "timeout=2h")
  blank_test=$(bk_send POST /settings/backups/test "$jar" "$out" \
    --data-urlencode "endpoint=$(bk_endpoint)" --data-urlencode "region=us-east-1" --data-urlencode "bucket=$BK_BUCKET" \
    --data-urlencode "prefix=verify/" --data-urlencode "force_path_style=true" --data-urlencode "schedule_time=03:30" \
    --data-urlencode "schedule_zone=UTC" --data-urlencode "retention_daily=5" --data-urlencode "timeout=2h")
  retention_after=$(db_psql "select retention_daily from backup_settings" | tr -d '[:space:]')
  [[ "$blank_status" == "200" && "$blank_test" == "200" && "$retention_after" == "5" ]] && blank=pass

  # backup-settings-csrf-403
  local csrf_status retention_csrf
  csrf_status=$(curl -sS -b "$jar" -o /dev/null -w '%{http_code}' -X PUT "$BASE/settings/backups" -H "HX-Request: true" \
    --data-urlencode "enabled=true" --data-urlencode "bucket=$BK_BUCKET" --data-urlencode "retention_daily=9" || true)
  retention_csrf=$(db_psql "select retention_daily from backup_settings" | tr -d '[:space:]')
  [[ "$csrf_status" == "403" && "$retention_csrf" == "5" ]] && csrf=pass

  # backup-settings-nonadmin-403 (needs GOFORGE_VERIFY_ENABLE_REGISTRATION=true)
  local na_reg="n/a" na_card="n/a" na_put="n/a" na_test="n/a"
  if [[ "$ENABLE_REGISTRATION" == "true" ]]; then
    local jar_u="$STATE_DIR/jar-user.txt"
    rm -f "$jar_u"
    curl -sS -c "$jar_u" -o /dev/null "$BASE/auth/register" || true
    na_reg=$(curl -sS -c "$jar_u" -b "$jar_u" -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/register" \
      --data-urlencode "csrf_token=$(csrf_from_jar "$jar_u")" --data-urlencode "email=verify-user@example.test" \
      --data-urlencode "password@$STATE_DIR/register.password" --data-urlencode "confirm_password@$STATE_DIR/register.password" || true)
    curl -sS -b "$jar_u" -c "$jar_u" -o "$page" "$BASE/settings/profile" >/dev/null || true
    if [[ "$(body_has "$page" 'id="backup-status"')" == "no" ]]; then na_card=hidden; else na_card=shown; fi
    na_put=$(bk_send PUT /settings/backups "$jar_u" "$out" --data-urlencode "enabled=false" --data-urlencode "bucket=x" \
      --data-urlencode "region=auto" --data-urlencode "retention_daily=7" --data-urlencode "timeout=2h" --data-urlencode "schedule_off=true")
    na_test=$(bk_send POST /settings/backups/test "$jar_u" "$out" --data-urlencode "bucket=x")
    nonadmin=fail
    [[ "$na_reg" == "303" && "$na_card" == "hidden" && "$na_put" == "403" && "$na_test" == "403" ]] && nonadmin=pass
    rm -f "$jar_u"
  fi

  # Disable keeps the destination and stops the scheduler.
  local dis_status dis_row dis_card dis_log
  dis_status=$(bk_send PUT /settings/backups "$jar" "$out" \
    --data-urlencode "endpoint=$(bk_endpoint)" --data-urlencode "region=us-east-1" --data-urlencode "bucket=$BK_BUCKET" \
    --data-urlencode "prefix=verify/" --data-urlencode "force_path_style=true" --data-urlencode "schedule_time=03:30" \
    --data-urlencode "schedule_zone=UTC" --data-urlencode "retention_daily=5" --data-urlencode "timeout=2h")
  sleep 1
  dis_row=$(db_psql "select enabled::text || ',' || bucket || ',' || (secret_access_key_sealed <> '')::text from backup_settings" 2>/dev/null | tr -d '[:space:]')
  curl -sS -b "$jar" -c "$jar" -o "$page" "$BASE/settings/profile" >/dev/null || true
  dis_card=$(body_has "$page" "Backups are off")
  dis_log=$(bk_count_log "backups disabled")
  [[ "$dis_status" == "200" && "$dis_row" == "false,$BK_BUCKET,true" && "$dis_card" == "yes" && "$dis_log" -ge 1 ]] && disable=pass

  # Audit rows hold field names only.
  local audit_file="$STATE_DIR/audit.txt" audit_rows audit_leak audit_fields
  install -m 600 /dev/null "$audit_file"
  db_psql "select details::text from audit_logs where action = 'backup.settings.update' order by created_at" >"$audit_file" || true
  audit_rows=$(grep -c . "$audit_file" || true)
  audit_leak=$(bk_leaks "$audit_file")
  audit_fields=$(python3 - "$audit_file" <<'PY'
import json, pathlib, sys
out = []
for line in pathlib.Path(sys.argv[1]).read_text().splitlines():
    if line.strip():
        d = json.loads(line)
        out.append("+".join(d.get("changed", [])) + ("" if set(d) == {"changed"} else "+EXTRA_KEYS"))
print(" | ".join(out))
PY
)
  [[ "$audit_rows" -ge 3 && "$audit_leak" == "no" && "$audit_fields" != *EXTRA_KEYS* ]] && audit=pass
  rm -f "$audit_file"

  local log_leak_file log_leak log_lines
  log_leak_file=$(mktemp "$STATE_DIR/app-log-scan.XXXXXX")
  app_logs >"$log_leak_file"
  log_lines=$(wc -l <"$log_leak_file" | tr -d ' ')
  log_leak=$(bk_leaks "$log_leak_file")
  rm -f "$log_leak_file"
  # An empty capture would make the leak check pass vacuously.
  [[ "$log_lines" -gt 0 && "$log_leak" == "no" ]] || harness=fail

  cat >"$verdict" <<EOF
harness=$harness
backup-settings-admin-edit=$edit
backup-settings-secret-masked=$masked
backup-settings-sealed=$sealed
backup-settings-live-apply=$live
backup-settings-test-connection=$test_ok
backup-settings-test-wrong-secret=$test_bad
backup-settings-blank-keeps=$blank
backup-settings-csrf-403=$csrf
backup-settings-nonadmin-403=$nonadmin
backup-settings-disable-keeps=$disable
backup-settings-audit-names-only=$audit
admin-login-status=$login_status
minio-bucket-status=$bucket_status
profile-status=$profile_status
profile-cache-control=$cache_control
profile-form-present=$form_present
profile-off-before-save=$off_before
save-status=$save_status
save-response-leaks-key=$save_leak
scheduler-log-lines-before=$sched_before
scheduler-log-lines-after=$sched_after
card-shows-new-schedule=$card_schedule
profile-leaks-key=$page_leak
access-key-hint-last4=$hint_shown
secret-state-line=$secret_state
$(printf '%s\n' "$sealed_out" | sed 's/^/sealed-/')
test-connection-status=$test_status
test-wrong-secret-status=$bad_status
test-wrong-secret-leaks=$bad_leak
blank-save-status=$blank_status
blank-test-status=$blank_test
retention-after-blank-save=$retention_after
csrf-missing-status=$csrf_status
retention-after-csrf-attempt=$retention_csrf
nonadmin-register-status=$na_reg
nonadmin-card=$na_card
nonadmin-put-status=$na_put
nonadmin-test-status=$na_test
disable-status=$dis_status
disable-row-enabled-bucket-secretset=$dis_row
disable-card-off=$dis_card
disable-log-lines=$dis_log
audit-rows=$audit_rows
audit-changed-fields=$audit_fields
audit-leaks-key=$audit_leak
app-log-lines=$log_lines
app-log-leaks-key=$log_leak
EOF
  chmod 644 "$verdict"
  rm -f "$page" "$out" "$hdr" "$jar"
  "${DOCKER[@]}" rm -f -v "$BK_MINIO" >/dev/null 2>&1 || true
  cat "$verdict"
  if [[ "$harness" != "ok" ]]; then
    exit 2
  fi
}

# --- drive-mcp-http (features/mcp-http.md) ---
# Tokens live only in $STATE_DIR as mode 600 files: the raw token and a curl
# header file ("Authorization: Bearer ...") passed with -H @file, so no token
# is ever on argv, in a verdict, or on stdout.

# mcp_save_token HTML NAME extracts the once-shown token from the created
# modal into $STATE_DIR/mcp-NAME.token and mcp-NAME.hdr. Prints yes or no.
mcp_save_token() {
  python3 - "$1" "$STATE_DIR/mcp-$2.token" "$STATE_DIR/mcp-$2.hdr" <<'PY'
import os, pathlib, re, sys
html = pathlib.Path(sys.argv[1]).read_text(errors="replace") if pathlib.Path(sys.argv[1]).exists() else ""
m = re.search(r'id="created-token"[^>]*>([^<]+)<', html)
if not m:
    print("no"); sys.exit(0)
tok = m.group(1).strip()
for path, text in ((sys.argv[2], tok), (sys.argv[3], "Authorization: Bearer " + tok)):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.write(fd, (text + "\n").encode()); os.close(fd)
print("yes" if tok.startswith("gf_") else "no")
PY
  rm -f "$1"
}

# mcp_post NAME OUT HDR JSON [curl args...] posts one JSON-RPC message to /mcp
# with token NAME ("none" sends no Authorization) and prints the status.
mcp_post() {
  local name="$1" out="$2" hdr="$3" body="$4"
  shift 4
  local -a auth=()
  [[ "$name" == "none" ]] || auth=(-H "@$STATE_DIR/mcp-$name.hdr")
  curl -sS -o "$out" -D "$hdr" -w '%{http_code}' -X POST "$BASE/mcp" "${auth[@]}" \
    -H "Content-Type: application/json" -H "Accept: application/json, text/event-stream" \
    --data-binary "$body" "$@" || true
}

# mcp_call NAME OUT TOOL ARGS_JSON [curl args...] sends tools/call.
mcp_call() {
  local name="$1" out="$2" tool="$3" args="$4"
  shift 4
  mcp_post "$name" "$out" /dev/null \
    "$(jq -cn --arg t "$tool" --argjson a "$args" '{jsonrpc:"2.0",id:1,method:"tools/call",params:{name:$t,arguments:$a}}')" "$@"
}

# mcp_text OUT prints the first text content of a tools/call result.
mcp_text() {
  jq -r '.result.content[0].text // ""' "$1" 2>/dev/null || true
}

mcp_is_error() {
  jq -r '.result.isError // false' "$1" 2>/dev/null || echo parse-error
}

# mcp_leaks FILE prints yes when FILE holds any token this drive minted.
mcp_leaks() {
  python3 - "$1" "$STATE_DIR" <<'PY'
import pathlib, sys
f = pathlib.Path(sys.argv[1])
text = f.read_text(errors="replace") if f.exists() else ""
toks = [p.read_text().strip() for p in pathlib.Path(sys.argv[2]).glob("mcp-*.token")]
print("yes" if any(t and t in text for t in toks) else "no")
PY
}

cmd_drive_mcp_http() {
  need_cmd curl
  need_cmd python3
  need_cmd jq
  need_cmd openssl
  install -d -m 755 "$EVIDENCE"
  install -d -m 700 "$STATE_DIR"
  if [[ ! -f "$STATE_DIR/register.password" ]]; then
    echo "missing $STATE_DIR/register.password. Run drive-register and drive-login first." >&2
    exit 1
  fi
  local verdict="$EVIDENCE/mcp-http-verdict.txt"
  local jar="$STATE_DIR/jar-mcp.txt" page="$STATE_DIR/mcp-page.html" out="$STATE_DIR/mcp-out.json" hdr="$STATE_DIR/mcp.headers"
  local admin_email harness=ok
  admin_email=$(printf '%s' "$ADMIN_EMAIL" | tr '[:upper:]' '[:lower:]')
  rm -f "$STATE_DIR"/mcp-*.token "$STATE_DIR"/mcp-*.hdr
  declare -A R=()

  local login_status
  login_status=$(bk_login "$jar" "$admin_email" "$STATE_DIR/register.password")
  [[ "$login_status" == "303" ]] || harness=fail
  local project_id env_id user_id
  project_id=$(db_psql "select id from projects where name = 'verify-smoke' order by created_at limit 1;" | tr -d '[:space:]')
  env_id=$(db_psql "select id from environments where project_id = '${project_id:-00000000-0000-0000-0000-000000000000}' order by created_at limit 1;" | tr -d '[:space:]')
  user_id=$(db_psql "select id from users where lower(email) = '$admin_email';" | tr -d '[:space:]')
  [[ -n "$project_id" && -n "$env_id" ]] || harness=fail

  # mcp-profile-card: the card shows the endpoint and the env-var snippet.
  local profile_status
  profile_status=$(curl -sS -b "$jar" -c "$jar" -o "$page" -w '%{http_code}' "$BASE/settings/profile" || true)
  R[profile-status]=$profile_status
  R[card-present]=$(body_has "$page" 'id="mcp-card"')
  R[card-endpoint]=$(body_has "$page" "$BASE/mcp")
  R[card-env-snippet]=$(body_has "$page" '${env:GOFORGE_MCP_TOKEN}')
  R[mcp-profile-card]=fail
  [[ "$profile_status" == "200" && "${R[card-present]}${R[card-endpoint]}${R[card-env-snippet]}" == "yesyesyes" ]] && R[mcp-profile-card]=pass

  # token-mcp-preset: the preset form is read+write, 90 days, no Never.
  local modal_status
  modal_status=$(bk_send GET "/settings/tokens/new?preset=mcp" "$jar" "$page")
  R[preset-modal-status]=$modal_status
  R[preset-hidden]=$(body_has "$page" 'name="preset" value="mcp"')
  R[preset-never]=$(body_has "$page" '>Never<')
  local create_write create_read create_full create_prot
  create_write=$(bk_send POST /settings/tokens "$jar" "$page" --data-urlencode "name=verify-mcp-write" \
    --data-urlencode "preset=mcp" --data-urlencode "expires_in=90")
  R[token-write-saved]=$(mcp_save_token "$page" write)
  create_read=$(bk_send POST /settings/tokens "$jar" "$page" --data-urlencode "name=verify-mcp-read" \
    --data-urlencode "preset=mcp" --data-urlencode "scopes=read" --data-urlencode "expires_in=30")
  R[token-read-saved]=$(mcp_save_token "$page" read)
  create_full=$(bk_send POST /settings/tokens "$jar" "$page" --data-urlencode "name=verify-legacy-full" \
    --data-urlencode "expires_in=30")
  R[token-full-saved]=$(mcp_save_token "$page" full)
  create_prot=$(bk_send POST /settings/tokens "$jar" "$page" --data-urlencode "name=verify-mcp-protonly" \
    --data-urlencode "preset=mcp" --data-urlencode "scopes=protected" --data-urlencode "expires_in=30")
  R[create-statuses]="write=$create_write read=$create_read full=$create_full preset-protected-only=$create_prot"
  R[write-scopes]=$(db_psql "select array_to_string(scopes, ',') from api_tokens where name = 'verify-mcp-write';" | tr -d '[:space:]')
  R[full-scopes]=$(db_psql "select array_to_string(scopes, ',') from api_tokens where name = 'verify-legacy-full';" | tr -d '[:space:]')
  R[write-expiry-days]=$(db_psql "select round(extract(epoch from expires_at - created_at) / 86400) from api_tokens where name = 'verify-mcp-write';" | tr -d '[:space:]')
  R[token-mcp-preset]=fail
  if [[ "$modal_status" == "200" && "${R[preset-hidden]}" == "yes" && "${R[preset-never]}" == "no" && "$create_write" == "201" \
    && "${R[token-write-saved]}" == "yes" && "${R[write-scopes]}" == "read,write" && "${R[write-expiry-days]}" == "90" \
    && "$create_prot" == "400" ]]; then
    R[token-mcp-preset]=pass
  fi
  [[ "${R[token-write-saved]}${R[token-read-saved]}${R[token-full-saved]}" == "yesyesyes" ]] || harness=fail

  # token-hint: the row shows gf_…last4, never the token. The list loads
  # into Profile with hx-get /settings/tokens.
  bk_send GET /settings/tokens "$jar" "$page" >/dev/null
  local write_hint
  write_hint="gf_…$(tail -c 5 "$STATE_DIR/mcp-write.token" | tr -d '\n')"
  R[hint-shown]=$(body_has "$page" "$write_hint")
  R[hint-page-leaks-token]=$(mcp_leaks "$page")
  R[hint-db-matches]=$([[ "$(db_psql "select token_hint from api_tokens where name = 'verify-mcp-write';" | tr -d '[:space:]')" == "$write_hint" ]] && echo yes || echo no)
  R[token-hint]=fail
  [[ "${R[hint-shown]}${R[hint-page-leaks-token]}${R[hint-db-matches]}" == "yesnoyes" ]] && R[token-hint]=pass

  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"verify-goforge","version":"1"}}}'
  local list='{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'

  # mcp-no-bearer-401 / mcp-cookie-only-401 / mcp-bad-bearer-401 / mcp-full-refused.
  local s www
  s=$(mcp_post none "$out" "$hdr" "$init"); www=$(header_value "$hdr" "WWW-Authenticate")
  R[no-bearer-status]=$s; R[no-bearer-www-authenticate]=$www
  R[mcp-no-bearer-401]=$([[ "$s" == "401" && "$www" == 'Bearer realm="goforge-mcp"'* ]] && echo pass || echo fail)
  s=$(mcp_post none "$out" "$hdr" "$init" -b "$jar")
  R[cookie-only-status]=$s
  R[mcp-cookie-only-401]=$([[ "$s" == "401" ]] && echo pass || echo fail)
  printf 'Authorization: Bearer gf_%s\n' "$(openssl rand -hex 24)" >"$STATE_DIR/mcp-bogus.hdr"
  s=$(mcp_post bogus "$out" "$hdr" "$init"); www=$(header_value "$hdr" "WWW-Authenticate")
  R[bad-bearer-status]=$s; R[bad-bearer-invalid-token]=$([[ "$www" == *'error="invalid_token"'* ]] && echo yes || echo no)
  R[mcp-bad-bearer-401]=$([[ "$s" == "401" && "${R[bad-bearer-invalid-token]}" == "yes" ]] && echo pass || echo fail)
  s=$(mcp_post full "$out" "$hdr" "$init"); www=$(header_value "$hdr" "WWW-Authenticate")
  R[full-status]=$s; R[full-error]=$(jq -r '.error.code // ""' "$out" 2>/dev/null || true)
  R[mcp-full-refused]=$([[ "$s" == "403" && "${R[full-error]}" == "insufficient_scope" && "$www" == *'error="insufficient_scope"'* ]] && echo pass || echo fail)

  # mcp-get-405: the stateless endpoint has no server-sent stream.
  s=$(curl -sS -o /dev/null -w '%{http_code}' -H "@$STATE_DIR/mcp-write.hdr" -H "Accept: text/event-stream" "$BASE/mcp" || true)
  R[get-status]=$s
  R[mcp-get-405]=$([[ "$s" == "405" ]] && echo pass || echo fail)

  # mcp-initialize: JSON response, no session id needed.
  s=$(mcp_post write "$out" "$hdr" "$init")
  R[initialize-status]=$s
  R[initialize-content-type]=$(header_value "$hdr" "Content-Type")
  R[initialize-server]=$(jq -r '.result.serverInfo.name // ""' "$out" 2>/dev/null || true)
  R[initialize-protocol]=$(jq -r '.result.protocolVersion // ""' "$out" 2>/dev/null || true)
  R[mcp-initialize]=$([[ "$s" == "200" && -n "${R[initialize-server]}" && "${R[initialize-content-type]}" == application/json* ]] && echo pass || echo fail)

  # mcp-read-inventory / mcp-write-inventory.
  s=$(mcp_post read "$out" /dev/null "$list")
  R[read-list-status]=$s
  R[read-tool-count]=$(jq -r '.result.tools | length' "$out" 2>/dev/null || echo 0)
  R[read-all-readonly]=$(jq -r '[.result.tools[] | .annotations.readOnlyHint == true] | all' "$out" 2>/dev/null || echo false)
  s=$(mcp_post write "$out" /dev/null "$list")
  R[write-list-status]=$s
  R[write-tool-count]=$(jq -r '.result.tools | length' "$out" 2>/dev/null || echo 0)
  R[write-tools-gated]=$(jq -r '[.result.tools[] | select(.annotations.readOnlyHint != true) | (.inputSchema.properties | has("dry_run") and has("approval_ref"))] | (length == 14 and all)' "$out" 2>/dev/null || echo false)
  R[mcp-read-inventory]=$([[ "${R[read-tool-count]}" == "24" && "${R[read-all-readonly]}" == "true" ]] && echo pass || echo fail)
  R[mcp-write-inventory]=$([[ "${R[write-tool-count]}" == "38" && "${R[write-tools-gated]}" == "true" ]] && echo pass || echo fail)

  # mcp-read-call: whoami answers as the caller's token.
  s=$(mcp_call read "$out" whoami '{}')
  R[whoami-read-scopes]=$(mcp_text "$out" | jq -r '.token.scopes | join(",")' 2>/dev/null || true)
  s=$(mcp_call write "$out" whoami '{}')
  R[whoami-write-scopes]=$(mcp_text "$out" | jq -r '.token.scopes | join(",")' 2>/dev/null || true)
  R[mcp-read-call]=$([[ "${R[whoami-read-scopes]}" == "read" && "${R[whoami-write-scopes]}" == "read,write" ]] && echo pass || echo fail)

  local env_args
  env_args=$(jq -cn --arg p "$project_id" --arg e "$env_id" '{project_id:$p,environment_id:$e,key:"VERIFY_MCP_KEY",value:"verify-value"}')

  # mcp-read-token-write-refused: a read token gets a tool error, not a call.
  mcp_call read "$out" set_env_var "$env_args" >/dev/null
  R[read-write-is-error]=$(mcp_is_error "$out")
  R[read-write-insufficient-scope]=$([[ "$(mcp_text "$out")" == insufficient_scope* ]] && echo yes || echo no)
  R[mcp-read-token-write-refused]=$([[ "${R[read-write-is-error]}" == "true" && "${R[read-write-insufficient-scope]}" == "yes" ]] && echo pass || echo fail)

  # mcp-dry-run: no dry_run means a plan; nothing is written.
  mcp_call write "$out" set_env_var "$env_args" >/dev/null
  R[dry-is-error]=$(mcp_is_error "$out")
  R[dry-flag]=$(mcp_text "$out" | jq -r '.dry_run' 2>/dev/null || true)
  R[dry-rows-after]=$(db_psql "select count(*) from env_variables where environment_id = '$env_id' and key = 'VERIFY_MCP_KEY';" | tr -d '[:space:]')
  R[mcp-dry-run]=$([[ "${R[dry-is-error]}" == "false" && "${R[dry-flag]}" == "true" && "${R[dry-rows-after]}" == "0" ]] && echo pass || echo fail)

  # mcp-approval-audit: approved run writes the row; the audit row carries
  # approval_ref, the outer client IP (X-Forwarded-For from a loopback peer),
  # and the MCP user agent.
  local ref approved_args
  ref="verify-mcp-$(openssl rand -hex 4)"
  approved_args=$(jq -c --arg r "$ref" '. + {dry_run:false, approval_ref:$r}' <<<"$env_args")
  mcp_call write "$out" set_env_var "$approved_args" -H "X-Forwarded-For: 203.0.113.7" >/dev/null
  R[approved-is-error]=$(mcp_is_error "$out")
  R[approved-rows-after]=$(db_psql "select count(*) from env_variables where environment_id = '$env_id' and key = 'VERIFY_MCP_KEY';" | tr -d '[:space:]')
  R[audit-row]=$(db_psql "select action || '|' || coalesce(host(ip_address), '') || '|' || split_part(coalesce(user_agent, ''), ' ', 1) from audit_logs where details->>'approval_ref' = '$ref' order by created_at limit 1;" | tr -d '[:space:]')
  R[mcp-approval-audit]=$([[ "${R[approved-is-error]}" == "false" && "${R[approved-rows-after]}" == "1" && "${R[audit-row]}" == "api_v1.set_env_var|203.0.113.7|goforge-mcp-http" ]] && echo pass || echo fail)
  R[approval-ref]=$ref

  # mcp-protected-refusal: a service row whose id is in GOFORGE_PROTECTED_SERVICES.
  local prot_id
  prot_id=$(mcp_call write "$out" whoami '{}' >/dev/null; mcp_text "$out" | jq -r '.protected_services[0] // ""' 2>/dev/null || true)
  R[protected-listed-in-whoami]=$([[ -n "$prot_id" ]] && echo yes || echo no)
  R[admin-user-found]=$([[ -n "$user_id" ]] && echo yes || echo no)
  if [[ -n "$prot_id" && -n "$user_id" ]]; then
    db_psql "insert into service_instances (id, user_id, template_name, name, status) values ('$prot_id', '$user_id', 'postgres', 'verify-protected', 'stopped') on conflict (id) do nothing;" >/dev/null
    mcp_call write "$out" update_service_config "$(jq -cn --arg s "$prot_id" '{service_id:$s,config:{name:"verify-renamed"},dry_run:false,approval_ref:"verify-protected"}')" >/dev/null
    R[protected-config-is-error]=$(mcp_is_error "$out")
    R[protected-config-names-protected]=$([[ "$(mcp_text "$out")" == *protected* ]] && echo yes || echo no)
    mcp_call write "$out" update_service_image "$(jq -cn --arg s "$prot_id" '{service_id:$s,dry_run:false,approval_ref:"verify-protected"}')" >/dev/null
    R[protected-image-is-error]=$(mcp_is_error "$out")
    mcp_call write "$out" restart_service "$(jq -cn --arg s "$prot_id" '{service_id:$s}')" >/dev/null
    R[protected-restart-dry-is-error]=$(mcp_is_error "$out")
    R[protected-restart-dry-flag]=$(mcp_text "$out" | jq -r '.dry_run' 2>/dev/null || true)
    # Second layer: the API refuses the same change for a write token.
    R[protected-api-status]=$(curl -sS -o /dev/null -w '%{http_code}' -X PATCH "$BASE/api/v1/services/$prot_id/config" \
      -H "@$STATE_DIR/mcp-write.hdr" -H "Content-Type: application/json" \
      --data-binary '{"config":{"name":"verify-renamed"},"dry_run":false,"approval_ref":"verify-protected"}' || true)
    R[protected-name-after]=$(db_psql "select name from service_instances where id = '$prot_id';" | tr -d '[:space:]')
  fi
  R[mcp-protected-refusal]=fail
  if [[ "${R[protected-config-is-error]:-}" == "true" && "${R[protected-config-names-protected]:-}" == "yes" \
    && "${R[protected-image-is-error]:-}" == "true" && "${R[protected-api-status]:-}" == "403" \
    && "${R[protected-name-after]:-}" == "verify-protected" ]]; then
    R[mcp-protected-refusal]=pass
  fi
  R[mcp-protected-restart-allowed]=$([[ "${R[protected-restart-dry-is-error]:-}" == "false" && "${R[protected-restart-dry-flag]:-}" == "true" ]] && echo pass || echo fail)

  # mcp-cross-origin-403: a browser request from another site is refused
  # before auth or tool code runs.
  s=$(mcp_post write "$out" /dev/null "$list" -H "Origin: https://evil.example.test" -H "Sec-Fetch-Site: cross-site")
  R[cross-origin-status]=$s
  R[mcp-cross-origin-403]=$([[ "$s" == "403" ]] && echo pass || echo fail)

  # token-rotate: new secret on the same row; the old one stops working at once.
  local write_id rotate_status old_status new_status new_whoami
  write_id=$(db_psql "select id from api_tokens where name = 'verify-mcp-write';" | tr -d '[:space:]')
  cp -p "$STATE_DIR/mcp-write.hdr" "$STATE_DIR/mcp-old.hdr"
  cp -p "$STATE_DIR/mcp-write.token" "$STATE_DIR/mcp-old.token"
  rotate_status=$(bk_send POST "/settings/tokens/$write_id/rotate" "$jar" "$page")
  R[rotate-status]=$rotate_status
  R[rotate-trigger]=$(body_has "$page" 'API Token Rotated')
  R[rotated-saved]=$(mcp_save_token "$page" rotated)
  old_status=$(mcp_post old "$out" /dev/null "$init")
  new_status=$(mcp_post rotated "$out" /dev/null "$init")
  mcp_call rotated "$out" whoami '{}' >/dev/null
  new_whoami=$(mcp_text "$out" | jq -r '.token.scopes | join(",")' 2>/dev/null || true)
  R[rotate-old-status]=$old_status; R[rotate-new-status]=$new_status; R[rotate-new-scopes]=$new_whoami
  R[rotate-same-row]=$(db_psql "select count(*) from api_tokens where name = 'verify-mcp-write' and id = '$write_id';" | tr -d '[:space:]')
  R[rotate-hint-changed]=$([[ "$(db_psql "select token_hint from api_tokens where id = '$write_id';" | tr -d '[:space:]')" == "gf_…$(tail -c 5 "$STATE_DIR/mcp-rotated.token" | tr -d '\n')" ]] && echo yes || echo no)
  R[token-rotate]=$([[ "$rotate_status" == "200" && "${R[rotated-saved]}" == "yes" && "$old_status" == "401" && "$new_status" == "200" \
    && "$new_whoami" == "read,write" && "${R[rotate-same-row]}" == "1" && "${R[rotate-hint-changed]}" == "yes" ]] && echo pass || echo fail)

  # token-revoke: the rotated token is refused after revoke.
  R[revoke-status]=$(bk_send DELETE "/settings/tokens/$write_id" "$jar" "$page")
  R[revoke-after-status]=$(mcp_post rotated "$out" /dev/null "$init")
  R[token-revoke]=$([[ "${R[revoke-status]}" == "200" && "${R[revoke-after-status]}" == "401" ]] && echo pass || echo fail)

  # token-audit: create, rotate, revoke rows with no token values in details.
  sleep 1
  R[audit-create-rows]=$(db_psql "select count(*) from audit_logs where action = 'api_token.create' and details->>'name' in ('verify-mcp-write','verify-mcp-read','verify-legacy-full');" | tr -d '[:space:]')
  R[audit-rotate-rows]=$(db_psql "select count(*) from audit_logs where action = 'api_token.rotate' and resource_id = '$write_id';" | tr -d '[:space:]')
  R[audit-revoke-rows]=$(db_psql "select count(*) from audit_logs where action = 'api_token.revoke' and resource_id = '$write_id';" | tr -d '[:space:]')
  local audit_dump
  audit_dump=$(mktemp "$STATE_DIR/mcp-audit.XXXXXX")
  db_psql "select details::text from audit_logs where action like 'api\_token.%' or details->>'approval_ref' = '$ref';" >"$audit_dump"
  R[audit-details-leak-token]=$(mcp_leaks "$audit_dump")
  rm -f "$audit_dump"
  R[token-audit]=$([[ "${R[audit-create-rows]}" == "3" && "${R[audit-rotate-rows]}" == "1" && "${R[audit-revoke-rows]}" == "1" && "${R[audit-details-leak-token]}" == "no" ]] && echo pass || echo fail)

  # No token in the app log.
  local logfile
  logfile=$(mktemp "$STATE_DIR/mcp-log.XXXXXX")
  app_logs >"$logfile" 2>/dev/null || true
  R[app-log-lines]=$(wc -l <"$logfile" | tr -d '[:space:]')
  R[app-log-leaks-token]=$(mcp_leaks "$logfile")
  rm -f "$logfile"

  {
    echo "harness=$harness"
    local k
    for k in mcp-profile-card token-mcp-preset token-hint mcp-no-bearer-401 mcp-cookie-only-401 mcp-bad-bearer-401 \
      mcp-full-refused mcp-get-405 mcp-initialize mcp-read-inventory mcp-write-inventory mcp-read-call \
      mcp-read-token-write-refused mcp-dry-run mcp-approval-audit mcp-protected-refusal mcp-protected-restart-allowed \
      mcp-cross-origin-403 token-rotate token-revoke token-audit; do
      echo "$k=${R[$k]:-fail}"
    done
    echo "---"
    for k in $(printf '%s\n' "${!R[@]}" | sort); do
      case "$k" in mcp-*|token-mcp-preset|token-hint|token-rotate|token-revoke|token-audit) continue ;; esac
      echo "$k=${R[$k]}"
    done
  } >"$verdict"
  chmod 644 "$verdict"
  redact_file "$verdict"
  local leak
  leak=$(mcp_leaks "$verdict")
  echo "verdict-leaks-token=$leak" >>"$verdict"
  rm -f "$page" "$out" "$hdr" "$jar" "$STATE_DIR"/mcp-*.token "$STATE_DIR"/mcp-*.hdr
  cat "$verdict"
  if [[ "$harness" != "ok" || "$leak" != "no" ]]; then
    exit 2
  fi
}


# --- drive-ui-a11y -----------------------------------------------------------
# axe plus keyboard checks in headless Chromium (features/ui-a11y.md). The
# pinned axe-core and playwright-core (ui-a11y/package-lock.json) install with
# npm ci into $UI_A11Y_NODE, outside the repo, and are reused while the lock
# file is unchanged. Runs on minis only; not part of CI.
UI_A11Y_NODE="$VERIFY_DIR/ui-a11y-node"

cmd_drive_ui_a11y() {
  need_cmd node
  need_cmd npm
  install -d -m 755 "$EVIDENCE"
  # Without GOFORGE_VERIFY_BASE the default base is 127.0.0.1:8080. Only drive
  # it when this run launched the goforge-verify stack (on minis the live
  # GoForge holds 8080).
  if [[ -z "${GOFORGE_VERIFY_BASE:-}" && ! -f "$STATE_DIR/stack.env" ]]; then
    echo "refusing: no $STATE_DIR/stack.env (run launch) and GOFORGE_VERIFY_BASE is unset; will not drive $BASE" >&2
    exit 1
  fi
  local src="$ROOT/.cursor/skills/verify-goforge/ui-a11y"
  install -d -m 755 "$UI_A11Y_NODE"
  if ! cmp -s "$src/package-lock.json" "$UI_A11Y_NODE/package-lock.json" \
    || [[ ! -f "$UI_A11Y_NODE/node_modules/axe-core/axe.min.js" || ! -d "$UI_A11Y_NODE/node_modules/playwright-core" ]]; then
    cp "$src/package.json" "$src/package-lock.json" "$UI_A11Y_NODE/"
    if ! (cd "$UI_A11Y_NODE" && npm ci --ignore-scripts --no-audit --no-fund) >"$EVIDENCE/ui-a11y-npm.log" 2>&1; then
      chmod 644 "$EVIDENCE/ui-a11y-npm.log"
      echo "npm ci failed for the pinned ui-a11y deps; see $EVIDENCE/ui-a11y-npm.log" >&2
      exit 2
    fi
    chmod 644 "$EVIDENCE/ui-a11y-npm.log"
  fi
  cp "$src/drive.mjs" "$UI_A11Y_NODE/drive.mjs"
  local pwfile=""
  [[ -f "$STATE_DIR/register.password" ]] && pwfile="$STATE_DIR/register.password"
  local email
  email=$(printf '%s' "$ADMIN_EMAIL" | tr '[:upper:]' '[:lower:]')
  local rc=0
  GOFORGE_UI_BASE="$BASE" GOFORGE_UI_EVIDENCE="$EVIDENCE" GOFORGE_UI_EMAIL="$email" \
    GOFORGE_UI_PASSWORD_FILE="$pwfile" node "$UI_A11Y_NODE/drive.mjs" || rc=$?
  redact_file "$EVIDENCE/ui-a11y-verdict.txt"
  redact_file "$EVIDENCE/ui-a11y-report.json"
  return "$rc"
}

evidence_list() {
  find "$EVIDENCE" -type f 2>/dev/null | sort
}

cmd_cleanup() {
  install -d -m 755 "$EVIDENCE"
  local before="$STATE_DIR/evidence-before.txt"
  install -d -m 700 "$STATE_DIR"
  evidence_list >"$before" || true
  if [[ -f "$STATE_DIR/stack.env" ]]; then
    compose down -v --remove-orphans >"$EVIDENCE/cleanup.log" 2>&1 || true
  else
    "${DOCKER[@]}" compose -p "$PROJECT" down -v --remove-orphans >"$EVIDENCE/cleanup.log" 2>&1 || true
  fi
  "${DOCKER[@]}" rm -f -v "$BK_MINIO" >>"$EVIDENCE/cleanup.log" 2>&1 || true
  redact_file "$EVIDENCE/cleanup.log"
  if "${DOCKER[@]}" network inspect traefik >/dev/null 2>&1; then
    local attached
    attached=$("${DOCKER[@]}" network inspect traefik -f '{{len .Containers}}' 2>/dev/null || echo 1)
    if [[ "$attached" == "0" ]]; then
      "${DOCKER[@]}" network rm traefik >>"$EVIDENCE/cleanup.log" 2>&1 || true
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
    drive-login) cmd_drive_login ;;
    psql) shift; cmd_psql "${1:-}" ;;
    check-sealed) shift; cmd_check_sealed "${1:-}" ;;
    check-backup-sealed) cmd_check_backup_sealed ;;
    drive-backup-settings) cmd_drive_backup_settings ;;
    drive-mcp-http) cmd_drive_mcp_http ;;
    drive-ui-a11y) cmd_drive_ui_a11y ;;
    cleanup) cmd_cleanup ;;
    *) usage ;;
  esac
}

main "$@"
