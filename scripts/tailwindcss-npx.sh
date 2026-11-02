#!/bin/sh
# Run the Tailwind v4 npm CLI with no package.json or node_modules in the repo.
#
#   sh scripts/tailwindcss-npx.sh VERSION [tailwindcss args...]
#
# `npx @tailwindcss/cli@VERSION` alone cannot build static/css/input.css:
# Tailwind 4 resolves `@import "tailwindcss"` from static/css upward, and
# npx installs the tailwindcss package in its own cache, off that path.
# This runs the CLI inside npx's environment with that cache's node_modules
# on NODE_PATH, which the v4 resolver also searches. The output is
# byte-identical to the standalone tailwindcss binary of the same version.
# Used by the Makefile fallback (no standalone CLI on PATH), CI, and release.
set -eu
if [ "$#" -lt 1 ]; then
	echo "usage: $0 VERSION [tailwindcss args...]" >&2
	exit 2
fi
version="$1"
shift
exec npx --yes -p "@tailwindcss/cli@$version" -- sh -c '
	bin=$(command -v tailwindcss)
	NODE_PATH="$(dirname "$(dirname "$bin")")${NODE_PATH:+:$NODE_PATH}" exec tailwindcss "$@"
' tailwindcss "$@"
