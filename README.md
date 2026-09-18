# Ship

A skill for Codex and Claude Code. Tell your agent to deploy, check, and update an app that already lives in **your** Vercel or Railway account. Neon is optional. Ship runs on your machine; there is no hosted backend.

![The local app list](docs/images/local-apps.png)

After setup, start a new chat and type `$ship` (Codex) or `/ship` (Claude Code). Then:

> Use Ship. Log into my Vercel account, attach this existing app as `demo`, check the plan, and deploy. Connect Neon only if this app needs it. Keep existing Secrets. Tell me when I need to approve a browser login.

Do not paste tokens. If you only have code and no cloud app yet, the agent should stop — this alpha cannot create cloud resources.

**[Get started](https://github.com/unix2dos/ship/blob/main/docs/FIRST-TRY.md)** — install, current limits (macOS Apple Silicon, existing Vercel Hobby app; Neon optional), and the first deploy.

[Alpha notes](docs/ALPHA.md) · [Product scope](PRODUCT.md) · [License](LICENSE)
