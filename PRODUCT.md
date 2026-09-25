# Carry · 产品设计提案

状态：2026-09-25 访谈后已确认的下一版方向，正在实施。这里描述目标与验收边界；[当前 Alpha](docs/ALPHA.md)仍只管理已有的 Vercel / Railway 应用，可选关联 Neon，不能创建云资源或部署到 VPS。[首次使用说明](docs/FIRST-TRY.md)按当前可用版本编写。

## 要解决的问题

一个人能写出应用，却经常卡在首次上线、更新、查错和保住数据。Carry 帮他把**自己的代码发布到自己的运行资源**，记录每次操作的目标与结果，并在失败或结果不明时核对现状，再决定继续或回退。

优先级是作者真实自用，其次才是外部开发者采用和公开作品展示。首个真实项目是 [Loop](https://github.com/unix2dos/loop)，首个目标主机是作者已经购买的 DMIT VPS。这个实例计划以 `loop.liuvv.com` 提供匿名访问；域名是本次验证样例，不是 Carry 为其他用户提供的公共子域名。

## 产品定位与取舍

Carry 是本地运行的部署与结果核对工具。用户已有的编码 Agent 可以调用它；没有 Agent 时，同一操作仍可从 CLI 发起。应用运行在用户自己的服务器或云账号中，凭证与操作记录留在用户控制的环境里。Carry 不运营集中式控制面。

第一条完整路径聚焦**已有 Linux VPS 上的一个 Web 应用**：从已有镜像或 Dockerfile 到 HTTPS 地址，再完成一次更新、一次故障恢复和实际业务检查。免费额度查询可作为已有云平台的辅助能力，不再作为产品承诺和平台选择的中心：额度、资格和用途由供应商决定，例如 Railway 的 Trial 与每月 Free 是两种条件，Vercel Hobby 限个人非商业用途。[Railway 计划](https://docs.railway.com/pricing/plans) · [Vercel Hobby](https://vercel.com/docs/plans/hobby) 已购服务器的固定费用是用户明确选择的成本；Carry 不替用户静默开通额外付费资源。

这不是再做一个功能齐全的服务器面板。Coolify 和 Dokploy 已覆盖应用、数据库及备份，Coolify 也提供 CLI；Kamal 已覆盖经 SSH 发布容器。Carry 需要验证的差别是：在**已有服务的主机**上先说明改动范围，用明确授权执行，记录镜像与配置，发布后核对真实结果，未知结果先查再重试，并保留可用的回退点。[Coolify CLI](https://coolify.io/docs/cli/deploy-applications) · [Coolify 数据库备份](https://coolify.io/docs/databases/backups) · [Dokploy 数据库](https://docs.dokploy.com/docs/core/databases) · [Kamal](https://kamal-deploy.org/docs/installation/)

这是一条差异化假设，不是已获用户需求验证。先在 Loop 上证明它确实比直接操作 Docker 或现有面板省心，再邀请外部开发者复现。

## 第一版交付边界

- **应用交付**：优先复用现成 OCI 镜像；否则使用项目的 Dockerfile 构建。单机运行采用 Docker Compose。资源紧张时在本机或 CI 构建目标架构镜像，让 VPS 只负责拉取和运行。暂不为每种语言实现原生进程发布器，也不自建构建平台。
- **主机接入**：通过用户已有 SSH 授权接入一台 Linux 主机；发布前读取系统架构、CPU/内存/磁盘、已有监听端口、容器运行环境及将影响的网络规则。首版不购买服务器、不重装系统、不接管无关服务。
- **公开访问**：用户自有域名、HTTPS、反向代理；只对外开放 Web 必需端口，应用与管理入口分开。部署目标和现有服务端口冲突时停止并说明原因。
- **持久数据**：应用声明的数据目录使用持久卷，更新容器不删除它。发布前保留上一个可运行镜像；首版回退只切换应用镜像，仍使用当前配置和密钥，不回退业务数据。
- **操作流程**：预检 → 展示变更及影响 → 用户授权 → 发布 → 读取服务状态与日志 → HTTP 检查 → 应用自身的业务检查。提交后失去响应时先查询当前镜像、容器和服务状态，不直接再次执行发布。
- **费用边界**：第一版默认只使用已购 VPS 和用户明确选择的免费模型。模型额度耗尽时暂停相关功能；没有付费模型或付费云资源的自动回退。Carry 的部署、状态、日志与恢复操作本身不依赖模型服务。

原生 `systemd` 发布可以用于个别不适合容器或迁移风险过高的现有服务，但不是第一版的第二套通用部署引擎。单台 VPS 不需要 Kubernetes、多节点调度或一个常驻的 Carry 服务器端控制面。

## 首个真实验证：Loop → DMIT

当前 Loop 是本地单用户 Node.js 工具，已有 [Dockerfile](https://github.com/unix2dos/loop/blob/main/Dockerfile)、`/healthz`，没有数据库或对象存储。现有公开模式没有访客身份：所有人能列出并读取全部轨迹，也能取得提交任务所用的同源令牌。镜像把运行记录放在 `/tmp`，换容器后无法保留。[请求处理](https://github.com/unix2dos/loop/blob/main/src/server.ts) · [镜像配置](https://github.com/unix2dos/loop/blob/main/Dockerfile)

因此公开部署先满足这些应用条件，再检验 Carry 的发布能力：

1. 只挂载可公开的示例 Markdown；不把作者私人笔记、主机目录或模型密钥放进镜像和网页响应。保留普通对话、工具轨迹与记录回看；Coding 练习不在首次公开范围。
2. 匿名访客凭浏览器持有的标识只看和继续自己的任务；清除浏览器数据后无法找回旧任务。记录在持久目录保留 **7 天**，到期清理。访客隔离覆盖列表、详情、导出与继续对话。
3. 共享的免费模型调用在服务端设全站与单访客上限；资源忙或额度耗尽时明确提示，不以新密钥、付费模型或自动重试绕过上限。
4. 模型首发使用 Z.AI 官方列为免费的 `glm-4.5-flash`：2026-09-25 已用专用 Key 验证 Chat Completions 返回和一次函数工具调用；`glm-4.7-flash` 在同次验证中出现超时和供应商过载。仍须通过 Loop 完成工具回执链与额度边界验收。供应商条款允许 API 集成到面向终端用户的应用，也要求管理终端用户行为。[价格表](https://docs.z.ai/guides/overview/pricing) · [API 条款](https://chat.z.ai/legal-agreement/terms-of-service)
5. 不把 OpenCode Zen 的 `muse-spark-1.3-contributor-free` 接给匿名公众：该免费项有期限，且 OpenCode 当前条款将服务限定为自己的内部使用。[免费项说明](https://opencode.ai/docs/zen/) · [使用条款](https://opencode.ai/legal/terms-of-service)
6. 在 DMIT 上保留已有 xray / 3x-ui。只读预检见到 Debian 13、x86_64、2 vCPU、约 2 GiB 内存、约 35 GiB 剩余磁盘；8443 已占用，Docker 尚未安装。安装 Docker 前核对其网络规则影响，部署后复核原服务。`loop.liuvv.com` 在本次核查时尚无 DNS 记录；公网 80/443 可达性尚未由独立外部网络证明。

### 验收标准

- 记录源码版本、镜像标识、目标主机、应用配置与操作结果；不输出模型密钥。
- `https://loop.liuvv.com` 证书正常；两个独立浏览器可分别提交真实任务，完成至少一次工具调用，并且无法列出、读取、导出或续接对方记录。
- 一次普通发布更新后，新版本生效，已有访客记录仍可回看；到期记录被清理。一次受控失败能通过日志定位并恢复；结果未知时先核对再操作。
- `/healthz` 只证明 HTTP 服务存活；另用一次真实模型任务验证业务链。免费模型不可用时，网页能说明原因而不会悄悄改用付费接口。
- 发布前后分别核对原有 xray 服务、监听端口和网络可达性；不能把 Loop 正常误当作原服务未受影响。

## 后续扩展触发条件

- **PostgreSQL**：第二个真实应用确实需要关系数据时，先支持连接用户已有数据库；若选择同机数据库，补持久卷、独立备份和实际恢复验证。Loop 不需要 PostgreSQL，不为展示功能而安装。
- **对象存储**：先分清应用文件桶和异地备份桶。真实应用需要文件上传或备份时，连接用户自有的 S3 兼容桶；不在小型 VPS 上先建对象存储服务。存储供应商可能要求账单开通，必须单独选择。
- **其他主机与平台**：只有单机路径在 Loop 及至少一个外部开发者项目上可复现，并且现有工具仍留下明确痛点时，再扩展供应商、原生发布或团队权限。

当前 Vercel、Railway、Neon Alpha 与其验证结论继续见 [Alpha 说明](docs/ALPHA.md)、[本地结果](validation/RESULTS.md)、[云端结果](validation/CLOUD-RESULTS.md)。它们不是上述 VPS 路径已经完成的证据。
