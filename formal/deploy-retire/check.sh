#!/usr/bin/env bash
# Model-check formal/deploy-retire/DeployRetire.tla with TLC on every config.
# Each config names the rules it is expected to break in its "expect-break:" line.
# For each config this checks (1) every other rule holds on the whole state space
# and (2) each expected rule really breaks. Any surprise exits 1.
#
#   formal/deploy-retire/check.sh            # all configs
#   formal/deploy-retire/check.sh FILE.cfg   # one config
#
# Needs java (11+) and curl on first run. TLC output goes to a temp dir outside
# the repo (kept on failure; path printed). Read results with /statechart.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
TLA_VERSION=1.7.4
TLA_SHA256=936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88
cache=${XDG_CACHE_HOME:-$HOME/.cache}/goforge-formal
jar=${TLA2TOOLS:-$cache/tla2tools-$TLA_VERSION.jar}
java=${JAVA:-java}

if [ ! -f "$jar" ]; then
  mkdir -p "$cache"
  curl -fsSL -o "$jar.tmp" "https://github.com/tlaplus/tlaplus/releases/download/v$TLA_VERSION/tla2tools.jar"
  echo "$TLA_SHA256  $jar.tmp" | sha256sum -c --quiet - && mv "$jar.tmp" "$jar"
fi
if [ -z "${TLA2TOOLS:-}" ]; then
  echo "$TLA_SHA256  $jar" | sha256sum -c --quiet - || { echo "tla2tools.jar checksum mismatch: $jar" >&2; exit 2; }
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/goforge-deploy-retire-tlc.XXXXXX")
cp "$here/DeployRetire.tla" "$work/"

if [ $# -gt 0 ]; then configs=("$@"); else
  configs=("$here/DeployRetire.cfg" "$here/DeployRetireBackToBack.cfg" "$here/DeployRetireDockerErrors.cfg"
           "$here/mutants/Pre95NoRemove.cfg" "$here/mutants/Pre96NoRecover.cfg")
fi

tlc() { # cfg out
  (cd "$work" && "$java" -XX:+UseParallelGC -cp "$jar" tlc2.TLC -workers 1 -metadir "$work/meta.$RANDOM" \
     -config "$1" DeployRetire.tla >"$2" 2>&1) || true
}

fail=0
for cfg in "${configs[@]}"; do
  name=$(basename "$cfg" .cfg)
  expect=$(sed -n 's/^\\\* expect-break: *//p' "$cfg" | sed 's/(none)//')
  # (1) every rule except the expected breaks holds
  keep="$work/$name.holds.cfg"
  awk -v drop=" $expect " '!($1=="INVARIANT" && index(drop, " " $2 " "))' "$cfg" >"$keep"
  tlc "$keep" "$work/$name.holds.out"
  if grep -q "Model checking completed. No error has been found." "$work/$name.holds.out"; then
    states=$(sed -n 's/^\([0-9,]*\) states generated, \([0-9,]*\) distinct states found.*/\2/p' "$work/$name.holds.out" | tail -1)
    echo "ok   $name: other rules hold ($states distinct states)"
  else
    echo "FAIL $name: a rule that should hold broke or TLC errored: $work/$name.holds.out"; fail=1
  fi
  # (2) each expected break really breaks
  for rule in $expect; do
    one="$work/$name.$rule.cfg"
    awk -v r="$rule" '$1!="INVARIANT" || $2==r' "$cfg" >"$one"
    tlc "$one" "$work/$name.$rule.out"
    if grep -q "Invariant $rule is violated" "$work/$name.$rule.out"; then
      echo "ok   $name: $rule breaks as expected"
    else
      echo "FAIL $name: $rule was expected to break and did not: $work/$name.$rule.out"; fail=1
    fi
  done
done

if [ "$fail" = 0 ]; then rm -rf "$work"; echo "all configs as expected"; else echo "TLC output kept in $work"; fi
exit "$fail"
