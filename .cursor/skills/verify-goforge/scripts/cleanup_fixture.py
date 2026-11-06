#!/usr/bin/env python3
"""Scoped disposable Docker/Postgres fixtures for drive-login-cleanup.

The selected Docker command must be explicitly supplied and is only used to
create/inspect the uniquely returned objects or this helper's named service.
Only disposable DB commands supplied by the caller are executed. No production
resource is enumerated or removed by labels/prefixes.
"""
import json
import os
import shlex
import subprocess
import sys
import uuid


def run(cmd):
    return subprocess.run(cmd, text=True, capture_output=True, check=True).stdout


def sql(query):
    command = os.environ.get("GOFORGE_VERIFY_PSQL", "")
    if not command:
        raise RuntimeError("GOFORGE_VERIFY_PSQL must select a disposable DB")
    return run(shlex.split(command) + ["-c", query])


def docker():
    command = os.environ.get("GOFORGE_VERIFY_DOCKER", "docker")
    host = os.environ.get("GOFORGE_TEST_DOCKER_HOST", "")
    configured = os.environ.get("DOCKER_HOST", "")
    if not host.startswith("unix:///tmp/") or host != configured or host not in command:
        raise RuntimeError("fixture requires matching DOCKER_HOST and GOFORGE_TEST_DOCKER_HOST on a temporary isolated daemon")
    return shlex.split(command)


def fixture(project, env, deployment, user):
    for value in (project, env, deployment, user):
        uuid.UUID(value)
    labels = {"goforge.managed": "true", "goforge.project.id": project, "goforge.environment.id": env}
    ids = []
    sql("insert into deployments (id,environment_id,status,trigger_type) values "
        f"('{deployment}','{env}','running','manual')")
    for kind in ("original", "restored"):
        name = f"verify-smoke-{kind}-{uuid.uuid4().hex[:10]}"
        args = ["run", "-d", "--name", name]
        for key, value in labels.items():
            args += ["--label", f"{key}={value}"]
        out = run(docker() + args + ["busybox:stable", "sleep", "300"]).strip()
        ids.append(out)
        docker_inspect = json.loads(run(docker() + ["inspect", out]))[0]
        out = docker_inspect["Id"]
        if kind == "restored":
            run(docker() + ["stop", out])
        sql("insert into containers (deployment_id,docker_id,name,image,status,labels) values "
            f"('{deployment}','{out}','{name}','busybox:stable','running','{json.dumps(labels)}')")
        print(f"fixture_{kind}={out}")
    service_created = run(docker() + ["run", "-d", "--name", f"verify-smoke-service-{uuid.uuid4().hex[:10]}",
                  "--label", "goforge.managed=true", "busybox:stable", "sleep", "300"]).strip()
    service = json.loads(run(docker() + ["inspect", service_created]))[0]["Id"]
    sql(f"insert into containers (docker_id,name,image,status) values ('{service}','verify-smoke-service','busybox:stable','running')")
    print(f"fixture_service={service}")
    print("fixture_rows_before=" + sql(f"select count(*) from containers where docker_id in ('{ids[0]}','{ids[1]}','{service}')").strip())
    with open(os.environ["GOFORGE_VERIFY_FIXTURE_STATE"], "w") as f:
        f.write("\n".join(ids + [service]) + "\n")


def orphan(project, env, user):
    for value in (project, env, user):
        uuid.UUID(value)
    deployment = uuid.uuid4()
    sql("insert into deployments (id,environment_id,status,trigger_type) values "
        f"('{deployment}','{env}','running','manual')")
    labels = {"goforge.managed": "true", "goforge.project.id": project, "goforge.environment.id": env}
    object_id = run(docker() + ["run", "-d", "--name", f"verify-smoke-orphan-{uuid.uuid4().hex[:10]}",
        "--label", "goforge.managed=true", "--label", f"goforge.project.id={project}",
        "--label", f"goforge.environment.id={env}", "busybox:stable", "sleep", "300"]).strip()
    object_id = json.loads(run(docker() + ["inspect", object_id]))[0]["Id"]
    # Simulate a prior completed parent-row delete, not a UI operation. The
    # retained project gives the automatic reconciler recoverable ownership.
    sql("insert into containers (deployment_id,docker_id,name,image,status,labels) values "
        f"('{deployment}','{object_id}','verify-smoke-orphan','busybox:stable','running','{json.dumps(labels)}')")
    sql(f"delete from environments where id = '{env}'")
    with open(os.environ["GOFORGE_VERIFY_FIXTURE_STATE"], "w") as f:
        f.write(object_id + "\n")
    print(f"orphan_fixture={object_id}")

def orphan_verify(user):
    with open(os.environ["GOFORGE_VERIFY_FIXTURE_STATE"]) as f:
        object_id = f.read().strip()
    if subprocess.run(docker() + ["inspect", object_id], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
        raise RuntimeError("orphan fixture still exists")
    rows = sql("select user_id::text,action,details->>'via' from audit_logs where action = 'container.orphan.remove' and details->>'resource_ref' = " + "'" + object_id + "'").strip().splitlines()
    expected = f"{user}|container.orphan.remove|system"
    if rows != [expected]:
        raise RuntimeError(f"owner-scoped system audit rows wrong: {rows}")
    print("orphan_host_gone=true")
    print("orphan_audit_user_id=" + user)
    print("orphan_audit_via=system")

def verify():
    with open(os.environ["GOFORGE_VERIFY_FIXTURE_STATE"]) as f:
        original, restored, service = f.read().splitlines()
    for object_id in (original, restored):
        code = subprocess.run(docker() + ["inspect", object_id], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode
        print(f"host_object_gone={object_id[:12]}:{str(code != 0).lower()}")
        if code == 0:
            raise RuntimeError("app fixture still exists")
    state = run(docker() + ["inspect", "--format", "{{.State.Running}}", service]).strip()
    print(f"service_preserved_running={str(state == 'true').lower()}")
    if state != "true":
        raise RuntimeError("one-click fixture was touched")
    print("app_container_rows=" + sql(f"select count(*) from containers where docker_id in ('{original}','{restored}')").strip())
    print("service_container_row=" + sql(f"select count(*) from containers where docker_id = '{service}'").strip())
    print("pending_intents=" + sql("select count(*) from container_cleanup").strip())
    if sql(f"select count(*) from containers where docker_id in ('{original}','{restored}')").strip() != "0":
        raise RuntimeError("stale container row")
    if sql(f"select count(*) from containers where docker_id = '{service}'").strip() != "1":
        raise RuntimeError("unrelated service row")
    if sql("select count(*) from container_cleanup").strip() != "0":
        raise RuntimeError("pending cleanup intent")


if len(sys.argv) == 2 and sys.argv[1] in ("verify",):
    verify()
elif len(sys.argv) == 6 and sys.argv[1] == "fixture":
    fixture(*sys.argv[2:])
elif len(sys.argv) == 5 and sys.argv[1] == "orphan":
    orphan(*sys.argv[2:])
elif len(sys.argv) == 3 and sys.argv[1] == "orphan-verify":
    orphan_verify(sys.argv[2])
else:
    raise SystemExit("usage: cleanup_fixture.py fixture PROJECT ENV DEPLOYMENT USER | verify | orphan PROJECT ENV USER | orphan-verify USER")
