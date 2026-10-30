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
  echo "usage: verify.sh launch|doctor|drive-register|drive-login|drive-backup-settings|check-backup-sealed|psql|check-sealed|cleanup" >&2
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
    cleanup) cmd_cleanup ;;
    *) usage ;;
  esac
}

main "$@"
