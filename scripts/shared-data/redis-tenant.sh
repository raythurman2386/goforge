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
# The user is reset on every run, so it gets exactly: ~<app>:* resetchannels
# &<app>:* +@all -@admin -@dangerous +info -scan -randomkey -pubsub, plus
# -function on redis 7 and later (6.2 has no FUNCTION). That denies
# FLUSHALL, FLUSHDB, KEYS, CONFIG, ACL, DEBUG, MONITOR, SWAPDB and the like;
# SCAN, RANDOMKEY, and PUBSUB (CHANNELS, NUMSUB, ...) are denied because they
# show other tenants' key or channel names, and FUNCTION because function
# libraries are shared by the whole instance (FUNCTION FLUSH would wipe
# everyone's). Keyed commands and scripts (EVAL) are checked against the key
# pattern, so a tenant gets NOPERM on another app's keys.
#
# Idempotent: a second run with the same password changes nothing visible; a
# run with a new password rotates it (existing connections stay
# authenticated until they reconnect). --revoke deletes the user and closes
# its connections; its keys are left in place. Every change is written to
# the instance's ACL file with ACL SAVE, so it survives a restart.
#
# Fails closed: on an instance started from a template without an ACL file
# (before #60; CONFIG GET aclfile is empty) the script exits 1 before
# creating, changing, or deleting any user. If ACL SAVE fails, ACL LOAD puts
# the live users back to the saved file and the script exits 1.
#
# Runs redis-cli inside the container as the default user, with the
# container's own REDIS_PASSWORD. Works on redis 6.2 and 7.x.
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

# Runs inside the container as the instance's default user. It changes
# nothing unless the instance loads an ACL file (CONFIG GET aclfile), so on
# an instance from before #60 (requirepass, no ACL file) no user is created
# or deleted, not even in memory. If ACL SAVE fails after the change, ACL
# LOAD puts the in-memory users back to what the file holds. redis-cli exits
# 0 on error replies, so every reply is checked. The app name is validated
# above, so it is safe in the ACL rule text; the password hash (create only)
# arrives on stdin and goes to redis-cli on its stdin.
# shellcheck disable=SC2016 # expanded by the container's sh
container_script='
set -eu
cli() { REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli --no-auth-warning "$@"; }
fail() { echo "$*"; exit 1; }
aclconf=$(cli config get aclfile | tr -d "\r")
[ "$(printf "%s\n" "$aclconf" | sed -n 1p)" = aclfile ] || fail "CONFIG GET aclfile failed: $aclconf"
if [ -z "$(printf "%s\n" "$aclconf" | sed -n 2p)" ]; then
	fail "the instance has no ACL file (started before #60); recreate it with the current redis template first. Nothing was changed"
fi
major=$(cli info server | tr -d "\r" | sed -n "s/^redis_version:\([0-9][0-9]*\)\..*/\1/p")
case "$major" in
	"" | *[!0-9]*) fail "could not read the redis version" ;;
esac
if [ "$MODE" = revoke ]; then
	cmd="ACL DELUSER $APP"
	want1=deleted
else
	hash=$(sha256sum | cut -d" " -f1)
	case "$hash" in
		*[!0-9a-f]* | "") fail "could not hash the password" ;;
	esac
	[ "${#hash}" -eq 64 ] || fail "could not hash the password"
	# resetchannels: on 6.2, reset keeps allchannels and &<app>:* is refused.
	# FUNCTION (7.0+) libraries are global, so tenants may not list or change them.
	rules="reset on #$hash ~$APP:* resetchannels &$APP:* +@all -@admin -@dangerous +info -scan -randomkey -pubsub"
	if [ "$major" -ge 7 ]; then rules="$rules -function"; fi
	cmd="ACL SETUSER $APP $rules"
	want1=OK
fi
replies=$(printf "%s\nACL SAVE\n" "$cmd" | cli)
first=$(printf "%s\n" "$replies" | sed -n 1p)
second=$(printf "%s\n" "$replies" | sed -n 2p)
# ACL DELUSER answers 1, or 0 for a user that does not exist (revoking
# twice is fine); ACL SETUSER answers OK.
case "$want1:$first" in
	deleted:0 | deleted:1 | OK:OK) ;;
	*) fail "$(echo "$cmd" | cut -d" " -f1-3) failed: $first" ;;
esac
if [ "$second" != OK ] || [ "$(printf "%s\n" "$replies" | wc -l)" -ne 2 ]; then
	loaded=$(cli acl load | tr -d "\r")
	fail "ACL SAVE failed: $second; ACL LOAD to restore the saved users: $loaded"
fi
echo "ok redis $major"
'

if $revoke; then
	out=$($docker_cmd exec -e MODE=revoke -e "APP=$app" "$container" sh -c "$container_script" 2>&1) || {
		echo "redis-tenant: revoke $app failed on $container: $out" >&2
		exit 1
	}
	echo "redis-tenant: user $app revoked on $container"
	exit 0
fi

password=$(cat)
if [ -z "$password" ]; then
	echo "redis-tenant: empty password on stdin" >&2
	exit 2
fi

out=$(printf '%s' "$password" | $docker_cmd exec -i -e MODE=create -e "APP=$app" "$container" sh -c "$container_script" 2>&1) || {
	echo "redis-tenant: failed for $app on $container: $out" >&2
	exit 1
}

echo "redis-tenant: user $app ready on $container (keys and channels $app:*, $out)"
