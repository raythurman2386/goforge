#!/bin/sh
# redis-tenant.sh: create, update, or revoke one app's ACL user on a GoForge
# redis service (for example the shared-redis instance).
#
#   printf '%s' "$APP_REDIS_PASSWORD" | scripts/shared-data/redis-tenant.sh <container> <app>
#   scripts/shared-data/redis-tenant.sh --revoke <container> <app>
#
# <container> is the service container name (goforge-service-<id>). <app> is
# the ACL user name and key prefix: lowercase letters, digits and _, starting
# with a letter, at most 63 characters. The app connects as
# redis://<app>:<password>@<container>:6379 and may only touch keys matching
# <app>:* and Pub/Sub channels matching <app>:*.
#
# The password is read from stdin, so it is not in this script's argv. It
# travels to the container on docker exec's stdin and is hashed there; only
# its SHA-256 reaches redis (on redis-cli's stdin, not its argv) and the ACL
# file.
#
# The user is reset on every run, so it gets exactly: ~<app>:* &<app>:*
# +@all -@admin -@dangerous +info -scan. That denies FLUSHALL, FLUSHDB, KEYS,
# CONFIG, ACL, DEBUG, MONITOR, SWAPDB and the like; SCAN is denied too
# because it lists every tenant's key names. Keyed commands and scripts
# (EVAL, FCALL) are checked against the key pattern, so a tenant gets
# NOPERM on another app's keys.
#
# Idempotent: a second run with the same password changes nothing visible; a
# run with a new password rotates it (existing connections stay
# authenticated until they reconnect). --revoke deletes the user and closes
# its connections; its keys are left in place. Every change is written to
# the instance's ACL file with ACL SAVE, so it survives a restart. An
# instance started from a template without an ACL file (before #60) answers
# ACL SAVE with an error and the script fails.
#
# Runs redis-cli inside the container as the default user, with the
# container's own REDIS_PASSWORD.
#
# Set DOCKER to override the docker command, for example DOCKER="sudo -n docker".
set -eu

usage() {
	echo "usage: printf '%s' \"\$PASSWORD\" | $0 <container> <app>" >&2
	echo "       $0 --revoke <container> <app>" >&2
	exit 2
}

revoke=false
if [ "${1:-}" = "--revoke" ]; then
	revoke=true
	shift
fi
[ "$#" -eq 2 ] || usage
container=$1
app=$2
docker_cmd=${DOCKER:-docker}

case "$app" in
	'' | [!a-z]* | *[!a-z0-9_]*)
		echo "redis-tenant: app name must match [a-z][a-z0-9_]*" >&2
		exit 2
		;;
esac
if [ "${#app}" -gt 63 ]; then
	echo "redis-tenant: app name longer than 63 characters" >&2
	exit 2
fi
case "$app" in
	default)
		echo "redis-tenant: $app is reserved" >&2
		exit 2
		;;
esac

if $revoke; then
	# The container script prints the replies; anything other than the
	# expected ones is a failure (redis-cli exits 0 on error replies).
	# ACL DELUSER answers 0 for a user that does not exist, so revoking
	# twice is fine.
	out=$($docker_cmd exec -e "APP=$app" "$container" sh -c '
		printf "ACL DELUSER %s\nACL SAVE\n" "$APP" |
			REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli --no-auth-warning' 2>&1) || out="exec failed: $out"
	case "$out" in
		[01]"
OK") ;;
		*)
			echo "redis-tenant: revoke $app failed on $container: $out" >&2
			exit 1
			;;
	esac
	echo "redis-tenant: user $app revoked on $container"
	exit 0
fi

password=$(cat)
if [ -z "$password" ]; then
	echo "redis-tenant: empty password on stdin" >&2
	exit 2
fi

# The password is hashed inside the container (sha256sum is in the official
# image) and only the hash is sent to redis-cli, on its stdin. The app name
# is validated above, so it is safe in the ACL rule text.
out=$(printf '%s' "$password" | $docker_cmd exec -i -e "APP=$app" "$container" sh -c '
	set -eu
	hash=$(sha256sum | cut -d" " -f1)
	case "$hash" in
		*[!0-9a-f]* | "") echo "could not hash the password"; exit 1 ;;
	esac
	[ "${#hash}" -eq 64 ] || { echo "could not hash the password"; exit 1; }
	printf "ACL SETUSER %s reset on #%s ~%s:* &%s:* +@all -@admin -@dangerous +info -scan\nACL SAVE\n" \
		"$APP" "$hash" "$APP" "$APP" |
		REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli --no-auth-warning' 2>&1) || out="exec failed: $out"
if [ "$out" != "OK
OK" ]; then
	echo "redis-tenant: failed for $app on $container: $out" >&2
	exit 1
fi

echo "redis-tenant: user $app ready on $container (keys and channels $app:*)"
