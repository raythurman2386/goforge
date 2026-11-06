#!/usr/bin/env python3
"""Authenticated, owner-scoped orphan Activity away/return assertions."""
import pathlib
import subprocess
import sys

state, jar, out = map(pathlib.Path, (sys.argv[1], sys.argv[3], sys.argv[4]))
base = sys.argv[2]
obj = (state / "orphan.fixture").read_text().split("=", 1)[1].strip()[:8]
page, code, checks = {}, {}, {}
for name, path in [
    ("before", "/settings/activity?source=system&action=container.orphan"),
    ("away", "/projects"),
    ("after", "/settings/activity?source=system&action=container.orphan"),
    ("ui", "/settings/activity?source=ui&action=container.orphan"),
]:
    f = state / f"orphan-{name}.html"
    code[name] = subprocess.run(
        ["curl", "-sS", "-b", str(jar), "-o", str(f), "-w", "%{http_code}", base + path],
        text=True, capture_output=True, check=True,
    ).stdout.strip()
    page[name] = f.read_text()
checks["orphan_database_correct"] = "orphan_audit_via=system" in (state / "orphan-retained.txt").read_text() and "orphan_audit_user_id=" in (state / "orphan-retained.txt").read_text()
checks["owner_system_before_row"] = all(s in page["before"] for s in ("container.orphan.remove", ">System<", obj))
checks["navigated_away_to_projects"] = "View and manage your application projects." in page["away"]
checks["owner_system_after_away_return"] = all(s in page["after"] for s in ("container.orphan.remove", ">System<", obj))
checks["system_not_classified_ui"] = "container.orphan.remove" not in page["ui"]
for route in code:
    checks[route + "_http"] = code[route] == "200"
with out.open("w") as verdict:
    for key, value in code.items():
        print(key + "_http=" + value, file=verdict)
    for key, value in checks.items():
        print(key + "=" + str(value).lower(), file=verdict)
for f in state.glob("orphan-*.html"):
    f.unlink()
print(out.read_text())
if not all(checks.values()):
    raise SystemExit(1)
