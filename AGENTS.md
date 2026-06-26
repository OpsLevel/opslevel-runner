# Agent Instructions

This project uses **bd** (beads) for issue tracking. Run `bd onboard` to get started.

## Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --status in_progress  # Claim work
bd close <id>         # Complete work
bd sync               # Sync with git
```

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

## Container / kind tooling

Prefer podman, fall back to docker. Under podman, kind needs the experimental
provider env var. Keep snippets pure shell (no Taskfile vars) so they can be
pasted into a terminal as-is:

```bash
if command -v podman &>/dev/null; then
  export KIND_EXPERIMENTAL_PROVIDER=podman
  cmd=podman
else
  cmd=docker
fi
```

Use `"$cmd"` for build/save/exec calls. Helper-image build+load logic lives in
`bin/build-helper-image.sh` (loads on rebuild or when absent in kind).

`crictl` is not present in kind nodes; query node images with:

```bash
"$cmd" exec <cluster>-control-plane ctr -n k8s.io images ls -q
```

