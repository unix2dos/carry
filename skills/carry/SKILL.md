---
name: carry
description: "Deploy and manage projects registered with the local Carry tool on a user-owned VPS or existing Vercel/Railway resources. Use for Carry bindings, release checks, logs, interrupted operations, and VPS image rollback."
---

# Carry

Use `carry` from PATH, or `~/.local/bin/carry` if PATH has not refreshed. Check `carry help`: the released Alpha may support only existing Vercel/Railway projects, while a source checkout can contain the newer VPS path. Read [the Alpha guide](../../docs/ALPHA.md) for installed cloud support and [the product plan](../../PRODUCT.md) for the VPS scope. The webpage and CLI share local records; the webpage is optional.

Installation registers this Skill for Codex and Claude Code. To set up a new computer, follow [the installation guide](https://github.com/unix2dos/carry/blob/main/docs/FIRST-TRY.md). Confirm which provider and executable version the user's project is bound to before choosing commands.

1. Run `carry list` and select the exact binding. For a new binding, verify the source directory and user-owned target before `register`; preserve an existing project's provider. VPS registration needs a known SSH alias, local and remote Docker, the container port and a user-owned HTTPS origin. A moved source uses `rebind NAME --source DIR` after checking the new tree. Vercel/Railway bindings keep their existing provider IDs; Neon is optional and requires all three IDs or none. Keep business secrets in Carry's private files, never source, chat, or `--vps-env`.
2. Run `carry status NAME`. For VPS, read the host resources, web-port preflight and app container state; also inspect unrelated existing services that the deployment might affect. For Vercel/Railway, report account plan and restrictions; Vercel Hobby is personal noncommercial and Trial is temporary.
3. An existing `allow_publish` authorization covers regular updates to its exact binding. For a requested update, run `carry publish NAME --detach`, then `carry reconcile NAME --wait` if still pending. Read the operation, container/provider state, and `carry check NAME` separately. `/healthz` proves HTTP access, not the application's business flow. A VPS image rollback uses `carry rollback NAME`; it keeps current secrets and data.

## Interrupted or failed work

`unknown`, a timeout, or a lost command response requires `reconcile` before another publish. The stored marker identifies the original provider deployment or VPS container. If it cannot be uniquely identified, preserve that uncertainty. Never remove operation records merely to bypass the duplicate-submit guard.

Use `logs` for diagnosis; it suppresses known credentials and common secret patterns, but review output before sharing it. Inspect source changes for migrations or other high-impact behavior before treating an update as routine. For VPS, check the original host services after Docker/network changes and distinguish a running container from working HTTPS and a real application task. Changes to billing, destructive resource actions, and unsupported lifecycle operations require a separate user decision; this skill does not authorize them.

## Local secret files

New installations use `~/.carry`; existing `~/.ship` and legacy configuration directories are reused without copying secrets or operations. `--state-dir` can select an isolated state. `secret save NAME KEY --stdin` stores plaintext in an owner-only per-project file and does not change cloud configuration. Pass values through stdin from an authorized source; inspect them through `secret list` / `secret check` rather than printing or attaching the files. Preserve reference IDs and unresolved writes during migration. Secret configuration changes require their own user authorization; `unknown` requires read-only reconciliation or a user decision, not another apply.

Ordinary Vercel source deployment retains existing provider Secrets and does not require importing or rewriting them. Report unreadable database configuration as unverified. Ordinary Secrets do not need a Neon binding. Writing `DATABASE_URL` through Carry still requires a registered Neon endpoint for target verification; other existing provider configuration is retained. Apply secret changes only when configuration changes are authorized; an unknown write still requires reconciliation or an explicit, evidence-backed disposition. Log filtering covers known values and common patterns, not every provider-hidden value.
