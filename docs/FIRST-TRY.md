# 独立试用：从空白本地安装到一次更新

目标：由没有参与 Ship 开发的人，用自己的电脑和云账号完成关联、检查、发布、更新与日志读取。作者代操作和作者已有账号的验收不计入独立试用。

## 试用范围

- 当前安装入口只验收 macOS Apple Silicon；Windows 尚未适配。
- 准备自己已有的 Vercel Hobby HTTP 容器应用与 Neon Free 数据库。应用需符合 Hobby 的个人非商业用途条件，并能访问 `/healthz` 和 `/readyz`。
- 应用源码包含 `Dockerfile.vercel` 与 `vercel.json`，后者在当前 Alpha 中只包含 `{"framework":"container"}`。
- 首次创建云项目、配置数据库连接与域名仍由供应商工具完成。Ship 本次试用验证的是对已有资源的日常管理。

## 1. 安装

准备 Go 1.24+、Node.js 24 和 npm：

```sh
git clone https://github.com/unix2dos/ship.git
cd ship
sh scripts/install.sh
"$HOME/.local/share/ship/bin/ship" serve --open
```

已有安装目录时，向安装脚本传入另一个不存在的绝对路径。默认本地记录在 `~/.ship`；业务密钥若由用户主动保存，会以权限受限的明文文件存放，详情见 [本地业务密钥](ALPHA.md#本地业务密钥)。首次关联已有应用不需要导入或重写平台上的 Secret。

## 2. 登录并关联自己的应用

从安装目录调用官方工具登录：

```sh
cd "$HOME/.local/share/ship"
mkdir -p "$HOME/.config/ship-vercel"
chmod 700 "$HOME/.config/ship-vercel"
env -u VERCEL_TOKEN -u VERCEL_ORG_ID -u VERCEL_PROJECT_ID \
  ./tools/node_modules/.bin/vercel login --global-config "$HOME/.config/ship-vercel"
env -u NEON_API_KEY -u NEON_PROFILE \
  ./tools/node_modules/.bin/neon login --config-dir "$HOME/.config/neon" --no-analytics
```

然后让能够执行本机命令的编码 Agent 阅读安装目录中的 `skills/ship/SKILL.md`，提供应用源码目录、现有访问地址和想使用的工作区。可直接说：

> 关联这个已有应用，核对我的 Vercel Hobby 和 Neon Free 资源，命名为 demo，并授权日常源码发布。沿用现有 Secret。Vercel 登录目录是 ~/.config/ship-vercel。先检查状态，再说明结果。

也可按 [Alpha 使用说明](ALPHA.md#vercel-接入的当前边界) 调用 `register`。凭证通过官方登录留在本机，不粘贴到聊天或反馈中。

## 3. 完成一次部署和更新

从安装目录运行：

```sh
./bin/ship status demo
./bin/ship check demo
./bin/ship publish demo --detach
./bin/ship reconcile demo --wait
./bin/ship check demo
./bin/ship logs demo
```

在浏览器打开应用地址，确认页面或版本信息符合本次源码。随后改一处能识别的新版本标记，再重复发布和核对。若应用有测试数据，确认更新后仍然存在；只使用自己准备的试验数据。

`deployed` 表示平台完成部署；访问检查和业务结果应分别确认。若出现 `unknown`，记录操作 ID 并使用 `reconcile`，不要用另一次 `publish` 替代原操作，也不要删除历史记录。

## 4. 返回最小反馈

记录以下内容即可：

| 项目 | 反馈 |
| --- | --- |
| 系统、芯片、Go / Node 版本 | |
| 测试的 Ship 提交版本 | |
| 安装与官方登录是否完成 | |
| 关联资源是否需要作者帮助 | |
| 首次发布、更新、浏览器访问是否通过 | |
| 更新后测试数据是否保留 | |
| 哪一步最费时间或最难理解 | |
| 失败命令、脱敏错误及操作 ID | |

不附上 `~/.ship/secrets/`、官方 CLI 登录文件、数据库连接串、本地页面的会话密钥或未经检查的完整日志。

独立试用尚待实际参与者完成；这份清单本身不代表外部验收通过。
