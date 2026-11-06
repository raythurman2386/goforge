#!/usr/bin/env bash
# Disposable PG and explicitly selected Docker only. No production app/resource.
set -euo pipefail
umask 077
ROOT=$(git rev-parse --show-toplevel)
BASE_DIR=$(mktemp -d /tmp/gf145-ui.XXXXXXXX)
PROOF=${GOFORGE_VERIFY_PROOF:-/tmp/goforge-verify/fix-proof}
PGPORT=${GOFORGE_VERIFY_PGPORT:-27435}
export DOCKER_HOST=${GOFORGE_TEST_DOCKER_HOST:-unix:///tmp/gf145-dind/run/docker.sock}
export GOFORGE_TEST_DOCKER_HOST=$DOCKER_HOST
for bin in initdb pg_ctl createdb psql openssl curl docker; do command -v "$bin" >/dev/null || { echo "missing $bin" >&2; exit 1; }; done
[[ $DOCKER_HOST == unix:///tmp/*/docker.sock ]] || { echo 'explicit temporary isolated unix Docker socket required' >&2; exit 1; }
export GOFORGE_VERIFY_ADMIN_EMAIL=verify-admin@example.test
export GOFORGE_VERIFY_ENABLE_REGISTRATION=false
HTTPPORT=${GOFORGE_VERIFY_HTTPPORT:-28457}
export GOFORGE_VERIFY_BASE=http://127.0.0.1:$HTTPPORT
export GOFORGE_VERIFY_PSQL_DSN=postgres://test:test@127.0.0.1:$PGPORT/goforge?sslmode=disable
export GOFORGE_VERIFY_STATE_DIR=$BASE_DIR/state
export GOFORGE_VERIFY_PROOF=$PROOF
export GOFORGE_VERIFY_DOCKER="docker -H $DOCKER_HOST"
export GOFORGE_VERIFY_FIXTURE_REPO=$ROOT/.cursor/skills/verify-goforge/scripts/cleanup_fixture.py
export GOFORGE_VERIFY_PSQL="psql --host=127.0.0.1 --port=$PGPORT --username=test --dbname=goforge --tuples-only --no-align"
export GOFORGE_VERIFY_FIXTURE_STATE=$BASE_DIR/state/cleanup.ids
export GOFORGE_VERIFY_ORPHAN_FIXTURE_STATE=$BASE_DIR/state/cleanup-orphan.ids
install -d -m 700 "$BASE_DIR/state"
install -d -m 700 "$PROOF"
cleanup() {
  rc=$?
  [[ ! -f "$BASE_DIR/app.pid" ]] || kill "$(cat "$BASE_DIR/app.pid")" 2>/dev/null || true
  [[ ! -d "$BASE_DIR/db" ]] || pg_ctl -D "$BASE_DIR/db" -m immediate stop >/dev/null 2>&1 || true
  exit "$rc"
}
trap cleanup EXIT
if [[ -f /tmp/goforge-verify/disposable.pgpass ]]; then rm -f /tmp/goforge-verify/disposable.pgpass; fi
printf test > "$BASE_DIR/pgpass"
initdb -D "$BASE_DIR/db" -U test --auth=trust --pwfile="$BASE_DIR/pgpass" >/dev/null
pg_ctl -D "$BASE_DIR/db" -l "$BASE_DIR/pg.log" -o "-p $PGPORT -h 127.0.0.1 -k $BASE_DIR" start >/dev/null
createdb -h 127.0.0.1 -p "$PGPORT" -U test goforge
cat > "$BASE_DIR/app.env" <<EOF
DATABASE_URL=postgres://test:test@127.0.0.1:$PGPORT/goforge?sslmode=disable
ENCRYPTION_KEY=$(openssl rand -hex 16)
SESSION_SECRET=$(openssl rand -hex 16)
CSRF_SECRET=$(openssl rand -hex 16)
WEBHOOK_SECRET=$(openssl rand -hex 16)
GOFORGE_HOST=127.0.0.1
GOFORGE_PORT=$HTTPPORT
GOFORGE_BASE_URL=$GOFORGE_VERIFY_BASE
GOFORGE_ENABLE_REGISTRATION=false
GOFORGE_ADMIN_EMAIL=verify-admin@example.test
GOFORGE_DEV_MODE=false
DOCKER_HOST=$DOCKER_HOST
LOG_FORMAT=json
EOF
set -a
# shellcheck source=/dev/null
. "$BASE_DIR/app.env"
set +a
go build -o "$BASE_DIR/goforge" "$ROOT/cmd/goforge"
(cd "$BASE_DIR" && ./goforge serve) >"$PROOF/app.log" 2>&1 &
echo $! >"$BASE_DIR/app.pid"
for _ in {1..60}; do
  curl -fsS "$GOFORGE_VERIFY_BASE/health/ready" >/dev/null 2>&1 && break
  kill -0 "$(cat "$BASE_DIR/app.pid")" 2>/dev/null || { echo 'disposable app failed to start' >&2; exit 2; }
  sleep 0.25
done
printf 'isolated_docker_socket=%s\ndisposable_pg_port=%s\n' "$DOCKER_HOST" "$PGPORT" >"$PROOF/isolation.txt"
DRIVER=$ROOT/.cursor/skills/verify-goforge/scripts/verify.sh
"$DRIVER" drive-register >"$PROOF/register.log"
"$DRIVER" drive-login >"$PROOF/login.log"
shutdown_command="kill $(cat $BASE_DIR/app.pid)"
eval "$shutdown_command"
for _ in {1..20}; do kill -0 "$(cat "$BASE_DIR/app.pid")" 2>/dev/null || break; sleep 0.1; done
python3 "$ROOT/.cursor/skills/verify-goforge/scripts/startup_orphan_fixture.py" >"$BASE_DIR/state/orphan.fixture"
(cd "$BASE_DIR" && ./goforge serve) >>"$PROOF/app.log" 2>&1 &
echo $! >"$BASE_DIR/app.pid"
startup_ready=0
for _ in {1..60}; do
  if curl -fsS "$GOFORGE_VERIFY_BASE/health/ready" >/dev/null 2>&1; then startup_ready=1; break; fi
  sleep 0.25
done
[[ $startup_ready == 1 ]] || { echo 'restarted disposable app not ready' >&2; exit 2; }
# Reconcile runs asynchronously with the readiness endpoint. Do not mistake a
# healthy HTTP process for a completed startup pass; bound the exact fixture wait.
orphan_id=$(cut -d= -f2 "$BASE_DIR/state/orphan.fixture")
orphan_removed=0
for _ in {1..120}; do
  if ! docker -H "$DOCKER_HOST" inspect "$orphan_id" >/dev/null 2>&1; then orphan_removed=1; break; fi
  kill -0 "$(cat "$BASE_DIR/app.pid")" 2>/dev/null || { echo 'disposable reconcile app stopped' >&2; exit 2; }
  sleep 0.25
done
[[ $orphan_removed == 1 ]] || { echo 'startup orphan was not removed by deadline' >&2; exit 1; }
GOFORGE_VERIFY_FIXTURE_STATE="$BASE_DIR/state/cleanup-orphan.ids" "$DRIVER" drive-orphan-activity >"$PROOF/activity.log"
"$DRIVER" drive-login-cleanup >"$PROOF/typed-delete.log"
chmod 644 "$PROOF"/*.txt "$PROOF"/*verdict* "$PROOF"/*.log 2>/dev/null || true
echo "PASS: disposable typed project delete and owner Activity away/back; evidence=$PROOF"
