#!/usr/bin/env python3
"""Population of one disposable owner/project and its startup orphan onto the selected engine."""
import os, subprocess, uuid

def docker(args):
    return subprocess.run(["docker", "-H", os.environ["DOCKER_HOST"]] + args, text=True, capture_output=True, check=True).stdout.strip()

def sql(args):
    result = subprocess.run(["psql", os.environ["GOFORGE_VERIFY_PSQL_DSN"], "-v", "ON_ERROR_STOP=1", "-q", "-tA"] + args, text=True, capture_output=True)
    if result.returncode:
        raise RuntimeError("disposable fixture SQL failed: " + result.stderr.replace(os.environ["GOFORGE_VERIFY_PSQL_DSN"], "[redacted]"))
    return result.stdout.strip()

def run_sql(command):
    args = os.environ["GOFORGE_VERIFY_PSQL"].split() + ["-c", command]
    return subprocess.run(args, text=True, capture_output=True, check=True).stdout.strip()

email = "verify-admin@example.test"
p = sql(["-c", "select id from projects where name='verify-smoke'"])
assert len(p) == 36, p
env = sql(["-c", "insert into environments (project_id,name) values ('%s','orphan-proof') on conflict (project_id,name) do update set name=excluded.name returning id" % p])
deployment = str(uuid.uuid4())
object_id = docker(["run", "-d", "--name", f"verify-smoke-orphan-{uuid.uuid4().hex[:8]}", "--label", "goforge.managed=true", "--label", f"goforge.project.id={p}", "--label", f"goforge.environment.id={env}", "busybox:stable", "sleep", "300"])
object_id = docker(["inspect", "--format", "{{.Id}}", object_id])
sql(["-c", "insert into deployments (id,environment_id,status,trigger_type) values ('%s','%s','running','manual')" % (deployment, env)])
sql(["-c", "insert into containers (deployment_id,docker_id,name,image,status) values ('%s','%s','verify-smoke-orphan','busybox:stable','running')" % (deployment, object_id)])
sql(["-c", "delete from environments where id='%s'" % env])
with open(os.environ["GOFORGE_VERIFY_ORPHAN_FIXTURE_STATE"], "w") as record:
    record.write(object_id + "\n")
print("orphan_fixture=" + object_id)
