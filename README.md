# Ship

Local deployment management for your apps, your cloud accounts, and your coding Agent.

Ship 在本地运行，让已有编码 Agent 通过 CLI 部署、检查和维护应用。网页提供应用列表、日志和发布入口，应用与数据库运行在用户自己的云账号中。

**当前为 Alpha。** 已验证的发布组合是 Railway Trial + Neon Free。正式 Railway Free 和付费账号的发布路径尚未验收，程序会停止这些账号的发布操作。软件采用 MIT 开源；云资源的费用取决于供应商套餐。

## 已实现

- 关联已有 Railway 服务与 Neon PostgreSQL，核对资源归属、数据库目标和费用条件。
- 从 Dockerfile 项目发布源码，查看状态、日志和应用访问检查。
- 保存本地操作记录，中断后核对原部署；结果未知时阻止重复提交。
- CLI、极简网页和配套 Skill 共用执行逻辑与状态。

核心操作直接调用用户授权的供应商接口，不依赖作者运营的在线后台。首次创建云资源、数据库迁移、无人值守维护和更多供应商尚未实现。

## 安装与启动

目前安装入口仅验收 **macOS arm64**，需要 Go 1.24+、Node.js 20.19+ 和 npm。

```sh
git clone https://github.com/unix2dos/ship.git
cd ship
sh scripts/install.sh
"$HOME/.local/share/ship/bin/ship" serve --open
```

安装程序把 Ship、固定版本的官方 CLI、使用说明和 Skill 放入独立目录，不需要 sudo。已有安装目录不会被覆盖；可以向脚本传入另一个尚不存在的绝对路径。

全新安装第一次打开时应用列表为空；从 UpOK 升级会沿用已有本地记录。先通过官方 CLI 登录自己的账号，再关联已有资源，具体命令见 [Alpha 使用说明](docs/ALPHA.md)。网页只监听 `127.0.0.1`；启动地址含本地会话密钥，请勿分享完整地址。

## 使用已有 Agent

让能够在本机执行命令的编码 Agent 阅读 [Ship Skill](skills/ship/SKILL.md)，选择项目后调用 CLI。无需 Agent 时也可直接操作：

```sh
cd "$HOME/.local/share/ship"
./bin/ship list
./bin/ship status demo
./bin/ship check demo
./bin/ship logs demo
./bin/ship publish demo --detach
./bin/ship reconcile demo --wait
```

`demo` 指用户已经登记并授权的本地项目。发布前程序会重新检查资源归属和费用条件。平台部署结果与应用访问结果分别记录：网络超时不等于部署失败。

## 验证范围

截至 2026-09-17：

- Go 检查、race 检测、`go vet`、前端语法与安装检查通过，包括改名后的旧状态与发布记录兼容检查。
- 安装后的工具从空白本地状态关联资源、完成真实发布，再由新进程核对；原 PostgreSQL 数据保留。[安装验收记录](validation/results/upok-install-report.json)
- Go、Python、Node、Rust 的 Linux/amd64 容器样例通过 29 组本地检查。[本地结果](validation/RESULTS.md)
- Go 样例完成真实云端部署、更新、自然休眠唤醒与一次配置故障恢复。[云端结果](validation/CLOUD-RESULTS.md)
- Go 容器另完成 Vercel Hobby + Neon 的独立验证，包含源码更新、数据保留和闲置后访问；用户已确认手动 Chrome 访问正常，自动化浏览器此前的客户端拦截仍未定位，正式 CLI 尚未接入 Vercel。[Vercel 结果](validation/VERCEL-RESULTS.md)

外部开发者独立复现、新用户 OAuth、正式 Free、其他本机系统和真实云间迁移仍未验收。早期 PORT 配置快照与运行值的差异保留为未决项。公开报告中的云项目、服务和数据库标识替换为占位符，测试时间、结果与源码指纹保留；本机凭证、个人讨论和原始私有记录不随仓库发布。

在源码目录运行开发检查：

```sh
go test -race ./cmd/ship
go vet ./cmd/ship
node --check cmd/ship/web/app.js
python3 validation/install-smoke.py "$HOME/.local/share/ship"
```

完整容器检查另需本地 Docker 和 OpenSSL，见 [验证说明](validation/README.md)。

## 设计与反馈

- [产品范围](PRODUCT.md) · [领域术语](CONTEXT.md) · [架构决策](docs/adr/)
- [平台与兼容性研究](docs/research/)

欢迎通过 Issues 提交复现步骤、操作系统、工具版本和脱敏错误信息。请先检查日志中是否包含令牌、数据库连接串或本地会话地址。

## 许可证

[MIT](LICENSE)。第三方依赖保留各自的许可证。
