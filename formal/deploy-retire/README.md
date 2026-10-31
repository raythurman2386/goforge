# formal/deploy-retire

TLA+ model of the GoForge redeploy grace and retire lifecycle (`internal/deploy/cleanup.go`).
How to run it, what each config expects, the coverage record, and the findings are in
[`.cursor/skills/verify-goforge-deploy-retire/SKILL.md`](../../.cursor/skills/verify-goforge-deploy-retire/SKILL.md).

```bash
formal/deploy-retire/check.sh
```

TLC and `/statechart` output always goes outside the repo; nothing generated here is committed.
