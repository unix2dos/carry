# 开始使用 Ship

Ship 让你的编码 Agent 部署、检查和维护你自己云账号中的应用，也提供本地网页。新项目默认使用 **Vercel + Neon**。

## 1. 安装 CLI 和 Skill

当前支持 **macOS Apple Silicon（M 系列）**，需要 [Node.js 24 或更新版本及 npm](https://nodejs.org/en/download)。不需要 Go、Git 或自己编译 Ship。

在终端执行：

```sh
curl -fsSL https://github.com/unix2dos/ship/releases/download/v0.1.0-alpha.1/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
ship --version
```

安装成功会显示 `ship v0.1.0-alpha.1`。安装器会：

- 下载预编译 CLI，核对 SHA-256，再安装固定版本的供应商工具。
- 提供 `~/.local/bin/ship`，并自动配置 zsh / bash 的 PATH。上面的 `export` 让当前终端立即生效，新终端无需再执行。
- 同时安装 Skill：Codex 使用 `~/.agents/skills/ship`，Claude Code 使用 `~/.claude/skills/ship`。

不需要 sudo。已有同名命令、其他来源的同名 Skill 或旧安装目录会明确提示，不会覆盖。相同版本可以重复运行安装命令以补齐 PATH 和 Skill。

## 2. 让 Agent 帮你接入

安装后在 Agent 中发起下一轮对话。Codex 可输入 `$ship`，Claude Code 可输入 `/ship`；如果没有出现，重启 Agent 后再试。

把下面这段和你的应用源码目录、现有访问地址一起发给它：

> 使用 Ship，帮我登录自己的 Vercel 和 Neon 账号，关联这个已有应用，命名为 demo。先核对账号、套餐和应用状态，再帮我完成一次部署。沿用已有数据库和 Secret；登录授权需要我操作时告诉我。

Agent 会按 Skill 调用本机 CLI，并引导你完成官方登录。凭证留在本机，不需要粘贴到聊天中。Hobby 的个人非商业用途条件与发布授权会在关联时说明。

**当前 Alpha 的边界：**你需要已有的 Vercel Hobby 应用和 Neon Free 数据库，且有可用访问地址。源码需包含 `Dockerfile.vercel`、内容为 `{"framework":"container"}` 的 `vercel.json`，以及 `/healthz`、`/readyz` 检查接口。首次创建云资源尚未实现；只有代码、还没有云资源时，Agent 应说明这个缺口。Windows 和 Intel Mac 安装包尚未发布。

## 3. 日常使用

在任意目录都可以运行：

```sh
ship list
ship status demo
ship publish demo --detach
ship reconcile demo --wait
ship check demo
ship logs demo
```

也可以直接告诉 Agent：“用 Ship 发布 demo”“检查 demo 状态”“查看 demo 日志”。后续更新沿用项目绑定；已有 Railway 项目仍使用 Railway。

想使用可视化界面时运行：

```sh
ship serve --open
```

网页只在本机运行。运行窗口需要保持开启；完整启动地址包含会话密钥，不要分享。

## 试用反馈

首次部署后修改一处版本标记，再部署一次，检查浏览器能否访问新版本、原有测试数据是否保留。结果未知时先让 Agent 执行 `reconcile`，不要重复发布。

反馈告诉我们：系统和芯片、`ship --version`、卡在哪一步、脱敏后的错误。不要附上云登录文件、数据库连接串或 `~/.ship/secrets/`。

[详细用法与存储说明](ALPHA.md) · [提交反馈](https://github.com/unix2dos/ship/issues)
