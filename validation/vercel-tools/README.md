# Vercel 容器验证工具

固定官方 Vercel CLI `59.20.0`，仅用于隔离样例验证，尚未接入 Carry 的正式发布命令。本次使用 macOS arm64 / Node.js `24.19.0`。Node.js 26 下登录曾返回 `fetch failed`，使用现有 Node.js 24 后授权成功；尚未证明失败根因仅为 Node 版本。

## 准备与登录

使用 Node.js 24，在仓库根目录运行：

```sh
(cd validation/vercel-tools && npm ci --no-audit --no-fund)
export CARRY_VERCEL_AUTH_DIR="$HOME/.config/carry-validation/vercel"
mkdir -p "$CARRY_VERCEL_AUTH_DIR"
chmod 700 "$CARRY_VERCEL_AUTH_DIR"
env -u VERCEL_TOKEN -u VERCEL_ORG_ID -u VERCEL_PROJECT_ID \
  VERCEL_TELEMETRY_DISABLED=1 NO_UPDATE_NOTIFIER=1 \
  ./validation/vercel-tools/node_modules/.bin/vercel login \
  --global-config "$CARRY_VERCEL_AUTH_DIR"
```

通过官方浏览器流程完成个人账号授权。凭证留在上述私有目录，后续命令显式提供 `--global-config` 和用户选定的 `--scope`。先读取用户、工作区及 Hobby 套餐，再检查网页 Usage 中的计算和镜像存储额度。Hobby 上 `vercel usage` 的计费数据可能不可用，这不能解释成用量为零。

## 部署前检查

测试目录放在仓库以外，复用 `validation/fixtures/go` 的 HTTP/PostgreSQL 约定，将 Dockerfile 命名为 `Dockerfile.vercel`。已有项目的 Other 框架设置可能阻止容器预设识别，可在样例 `vercel.json` 中显式声明：

```json
{"framework": "container"}
```

使用 `vercel deploy --dry --json` 核对框架为 Container、文件列表只有预期源码，再提交到明确的隔离项目。数据库连接串与验证令牌通过 `vercel env add --sensitive` 的标准输入写入运行时 Secret，避免放入源码、命令参数或日志。设置 `PORT=8080`，保留数据库 TLS 校验。

每次提交先记录唯一操作标记；响应未知时查找原部署。验收同时要求容器构建证据、真实健康/数据库接口和数据保留，不能把平台 Ready 当成应用已经运行。

本工具安装、登录和预检不代表容器路径已经验收通过。实际账号验证结果单独记录；付费或用途条件不满足时停止。

来源：[CLI 登录](https://vercel.com/docs/cli/login)、[全局参数](https://vercel.com/docs/cli/global-options)、[容器部署](https://vercel.com/docs/functions/container-images)、[前期研究](../../docs/research/2026-09-17-vercel-hobby-evaluation.md)。
