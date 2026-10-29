#!/bin/sh
# pg-tenant.sh: create or update one app's role and database on a GoForge
# postgres service (for example the shared-pg instance).
#
#   printf '%s' "$APP_DB_PASSWORD" | scripts/shared-data/pg-tenant.sh <container> <app>
#
# <container> is the service container name (goforge-service-<id>). <app> is
# the role and database name: lowercase letters, digits and _, starting with
# a letter, at most 63 characters. The password is read from stdin so it is
# not in this script's argv or in the psql command line; it travels to psql
# on its stdin.
#
# Idempotent: a second run with the same password changes nothing visible; a
# run with a new password rotates it. The role gets LOGIN only (no superuser,
# createdb, or createrole). The database is owned by the role, and PUBLIC
# loses CONNECT on it so other tenants cannot open it.
#
# Set DOCKER to override the docker command, for example DOCKER="sudo -n docker".
set -eu

usage() {
	echo "usage: printf '%s' \"\$PASSWORD\" | $0 <container> <app>" >&2
	exit 2
}

[ "$#" -eq 2 ] || usage
container=$1
app=$2
docker_cmd=${DOCKER:-docker}

case "$app" in
	'' | [!a-z]* | *[!a-z0-9_]*)
		echo "pg-tenant: app name must match [a-z][a-z0-9_]*" >&2
		exit 2
		;;
esac
if [ "${#app}" -gt 63 ]; then
	echo "pg-tenant: app name longer than 63 characters" >&2
	exit 2
fi
case "$app" in
	postgres | pgadmin | public | template0 | template1 | pg_*)
		echo "pg-tenant: $app is reserved" >&2
		exit 2
		;;
esac

password=$(cat)
if [ -z "$password" ]; then
	echo "pg-tenant: empty password on stdin" >&2
	exit 2
fi
# The password goes into one psql \set line, so a line break would let the
# rest of it run as psql input. Refuse control characters outright.
if [ "$(printf '%s' "$password" | LC_ALL=C tr -d '[:cntrl:]')" != "$password" ]; then
	echo "pg-tenant: password contains a control character (line break, tab, ...)" >&2
	exit 2
fi
# psql \set takes a single-quoted value where \ starts an escape and '' is
# a quote: double every backslash, then every quote.
quoted=$(printf '%s' "$password" | sed -e 's/\\/\\\\/g' -e "s/'/''/g")

# psql runs as the instance admin from the container's own environment.
# \gexec runs each generated statement; format(%I, %L) quotes the names and
# the password literal.
{
	printf '%s\n' '\set ON_ERROR_STOP on' "\\set app '$app'"
	printf "\\\\set pw '%s'\n" "$quoted"
	cat <<'SQL'
SELECT format('CREATE ROLE %I LOGIN', :'app')
 WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'app') \gexec
SELECT format('ALTER ROLE %I WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD %L', :'app', :'pw') \gexec
SELECT format('CREATE DATABASE %I OWNER %I', :'app', :'app')
 WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = :'app') \gexec
SELECT format('REVOKE ALL ON DATABASE %I FROM PUBLIC', :'app') \gexec
SELECT format('GRANT CONNECT, TEMPORARY ON DATABASE %I TO %I', :'app', :'app') \gexec
SQL
} | $docker_cmd exec -i "$container" sh -c 'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null

echo "pg-tenant: role and database $app ready on $container"
