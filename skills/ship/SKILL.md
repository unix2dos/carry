---
name: ship
description: "Manage projects registered with the local ship tool: inspect real Railway/Vercel and Neon status, publish source, inspect logs, and reconcile interrupted operations. Use for this tool's registered projects; resource creation and database migration are not implemented in its internal alpha."
---

# Ship

Use the local CLI from this repository (`bin/ship` after building). Read [the alpha guide](../../docs/ALPHA.md) when installation, authentication, project registration, or supported scope is unclear. The webpage and CLI share the same local records; opening the webpage is optional.

1. Run `ship list` and select the project matching the user's request. For a new binding, establish its source directory and exact resource ownership before `register`. Keep cloud login credentials in the official CLI stores; resource IDs belong in the binding. Ship-owned business secrets are stored separately in private local files, never in source or chat.
2. Run `ship status NAME`. Report the observed account plan and use restrictions; Vercel Hobby requires personal noncommercial use. Trial is a time-limited test path, not proof of a formally validated Free combination. The executable performs fresh ownership and billing checks before each publish.
3. For a requested source update, use `ship publish NAME --detach`, then `ship reconcile NAME --wait`. An existing `allow_publish` authorization covers regular updates to that binding; do not ask for it again. Grant or revoke project permissions with `authorize` only within the user's stated scope. Accept Trial conditions only if the user has already accepted them or confirms that choice now.
4. Read the resulting operation and application checks separately. `deployed` means the provider finished deployment; claim application accessibility only when the checks passed. Sleep, a failed request, and a failed deployment are different observations.

## Interrupted or failed work

`unknown`, a timeout, or a lost command response requires `reconcile`, not another `publish`. The stored marker identifies the original cloud deployment. If reconciliation cannot uniquely identify it, preserve that uncertainty and discuss it with the user. Never remove operation records merely to bypass the duplicate-submit guard.

Use `logs` for diagnosis; it suppresses known credentials and common secret patterns, but review output before sharing it. Inspect source changes for migrations or other high-impact behavior before treating an update as routine. Changes to billing, destructive resource actions, and unsupported lifecycle operations require a separate user decision; this skill does not authorize them.

## Local secret files

The default state is `~/.ship`; `--state-dir` can select an isolated state. `secret save NAME KEY --stdin` stores plaintext in an owner-only per-project file and does not change cloud configuration. Pass values through stdin from an authorized source; inspect them through `secret list` / `secret check` rather than printing or attaching the files. Preserve reference IDs and unresolved writes during migration. Secret configuration changes require their own user authorization; `unknown` requires read-only reconciliation or a user decision, not another apply.

Ordinary Vercel source deployment retains existing provider Secrets and does not require importing or rewriting them. Report unreadable database configuration as unverified. Apply secret changes only when configuration changes are authorized; an unknown write still requires reconciliation or an explicit, evidence-backed disposition. Log filtering covers known values and common patterns, not every provider-hidden value.
