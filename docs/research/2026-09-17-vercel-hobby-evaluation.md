# Vercel Hobby 与 UpOK 的适配评估

核查日期：2026-09-17。范围：Vercel 官方文档、定价页和开放源码计划。通过 agent-reach 的 Exa、Jina Reader 读取，定价表与构建限制再以官方原页交叉核对。本次没有登录账号、部署应用、修改云资源或进行费用设置。

后续实测更新（同日）：经用户授权，Go HTTP 容器已在个人 Hobby 账号完成构建、Neon 读写、源码更新和闲置后访问检查。需要显式 Container 预设；当前 Chrome 的默认域名访问存在客户端拦截，浏览器验收未通过。实际证据与限制见 [Vercel 验证结果](../../validation/VERCEL-RESULTS.md)，不将文档支持范围直接等同于所有语言或账号已验收。

## 结论

**Vercel Hobby 值得成为个人非商用应用的候选路径；它现在支持 HTTP 容器，不能再按“不支持 Docker、Rust 或 WebSocket”的旧印象排除。它仍然不是免费通用 VPS，也不适合作为所有 UpOK 用户的统一免费默认。**

前一句依据是当前运行时与 Container Images 文档；后一句是由 Hobby 的用途、请求生命周期和额度限制得出的产品判断，尚未在 UpOK 中实测。[官方运行时](https://vercel.com/docs/functions/runtimes)、[容器部署](https://vercel.com/docs/functions/container-images)、[Hobby 套餐](https://vercel.com/docs/plans/hobby)

## 1. 最大边界：Hobby 仅限个人非商用

官方限制不只是“网站有没有收费按钮”。以参与项目生产的任何人的经济收益为目的，包含付费雇员或顾问编写项目，均可能构成 commercial usage；示例包括收款、宣传产品或服务销售、收费制作/更新/托管网站、主要用于联盟推广、刊登广告。单纯请求捐赠不算商业使用。商业用途要求 Pro 或 Enterprise；有歧义时官方要求联系支持确认。[Fair Use Guidelines](https://vercel.com/docs/limits/fair-use-guidelines#commercial-usage)

**对 UpOK 的推论：UpOK 自身开源、免费，不会让它部署的用户应用自动取得 Hobby 非商用资格。** 商用 SaaS、接单网站、公司业务应用不能因为通过 UpOK 部署就默认选择 Hobby。

## 2. 当前主要免费额度

以下为 Hobby 当前公开额度，按月提供；额度按所属 Hobby 账号/团队汇总理解，不应按每个项目重复计算。实际账号 Usage 页和资源限制仍需验收。[Hobby 套餐](https://vercel.com/docs/plans/hobby)、[定价总表](https://vercel.com/pricing)

| 资源 | Hobby 额度 | 如何理解 |
| --- | --- | --- |
| Fast Data Transfer | 100 GB/月 | 平台向用户交付内容的流量 |
| Fast Origin Transfer | 10 GB/月 | 源站计算与交付网络间的流量；不是额外 10 GB 用户流量赠送 |
| Edge Requests | 100 万次/月 | 静态和动态请求都可能消耗，不能等同于访客人数 |
| Function Invocations | 100 万次/月 | 到达函数的调用，包含失败调用 |
| Fluid Active CPU | 4 CPU 小时/月 | 代码实际占用 CPU 的时间；等待数据库/API 不计入 CPU |
| Fluid Provisioned Memory | 360 GB·小时/月 | 分配内存 × 实例处理请求的时间，I/O 等待仍计入 |
| VCR 镜像存储，Beta | 10 GB/月 included | 来自主定价表的 Hobby 列；不是数据库容量 |
| 构建 | Basic 构建机 included | 2 vCPU、8 GB 内存、32 GB 磁盘；同时 1 次构建 |
| 发布频率与单次构建 | 100 次部署/天；单次构建最多 45 分钟 | 频率/时间约束，不等于服务器可运行时长 |

CPU、内存计费口径与请求间无在途工作时不计 CPU/内存费用见 [Fluid pricing](https://vercel.com/docs/functions/usage-and-pricing)。构建配置见 [Managing Builds](https://vercel.com/docs/builds/managing-builds)，单次构建和发布上限见 [Limits](https://vercel.com/docs/limits)。当前核查的主要官方页面没有确认常见旧资料所称的“6,000 构建分钟/月”，因此本报告不采用该数字。

### 不要混用 Legacy 和 Fluid

Fluid 自 2025-04-23 起对新项目默认开启。旧式非 Fluid 文档仍列 Hobby 100 GB·小时和 10 万次函数调用；它们不是当前 Fluid 的免费额度。是否使用 Fluid 要看项目和运行时，不能只看账号套餐。[Fluid](https://vercel.com/docs/fluid-compute)、[Legacy pricing](https://vercel.com/docs/functions/usage-and-pricing/legacy-pricing)

### 用完额度会怎样

Hobby 不能购买额外用量；当前 Hobby 说明是多数功能超额后需等到 30 天过去才可再次使用，部分功能暂停周期不同。不能承诺一定在下个自然月 1 日恢复，也不能把所有限制归纳为只停止新构建。Pro 才有按需付费路径，升级必须是用户明确选择。[定价 FAQ](https://vercel.com/pricing)、[Hobby billing cycle](https://vercel.com/docs/plans/hobby#hobby-billing-cycle)

## 3. 已不只是静态网页或 JavaScript 函数

| 路径 | 当前官方状态 | 对兼容性的影响 |
| --- | --- | --- |
| Node.js、Python | 官方运行时；Python 支持 ASGI/WSGI | 可部署 API/动态网站，仍需满足运行生命周期和依赖限制 |
| Rust | 官方 Rust runtime，所有套餐 Beta，使用 `vercel_runtime` | 不应再描述成只有社区支持；原生函数入口涉及平台适配 |
| Go | 官方 Go runtime，所有套餐 Beta；支持 `net/http`、chi、gin HTTP 服务，也保留 `/api` handler 路径 | `framework=go`，根 `go.mod`，识别 `main.go`、`cmd/api/main.go`、`cmd/server/main.go`，监听 `PORT` |
| Docker/OCI HTTP 容器 | Container Images 所有套餐 Beta | 可云端构建容器，语言自由度更接近 UpOK 既有契约 |

来源：[Runtimes](https://vercel.com/docs/functions/runtimes)、[Rust](https://vercel.com/docs/functions/runtimes/rust)、[Go](https://vercel.com/docs/functions/runtimes/go)、[Container Images](https://vercel.com/docs/functions/container-images)。

运行时汇总页对 Go 仍使用较旧的单 handler 描述，而 Go 专页已列标准 HTTP 服务；本报告采用更具体的 Go 专页。Fluid 支持列表目前没有列 Go，虽然 Go 专页已能接标准服务器，因此**原生 Go 路径的 Fluid 资格与额度不能只凭其它运行时推定**。容器页则明确采用 Functions 的 Active CPU 模型。[Go 专页](https://vercel.com/docs/functions/runtimes/go)、[Fluid runtime support](https://vercel.com/docs/fluid-compute#available-runtime-support)

### 容器的具体契约

- 根目录 `Dockerfile.vercel` 或 `Containerfile.vercel` 会被检测，部署时云端构建、上传 VCR 并路由请求；本地 `vercel dev` 才要求本机 Docker。
- 容器必须提供 HTTP 服务，默认端口 80，可用 `PORT` 环境变量覆盖。
- 生产环境 5 分钟无流量、预览环境 30 秒无流量后缩容；退出前有 SIGTERM 和 30 秒清理窗口。
- 使用 Functions 的限制和 Active CPU 计费，当前不支持 Secure Compute、Static IPs。

来源：[Container Images，更新于 2026-07-07](https://vercel.com/docs/functions/container-images)。

VCR 的存储接受镜像总压缩大小最多 15 GB、单个压缩 layer 最多 500 MB；Hobby 每项目 10 个仓库、每仓库 50 个镜像。**这是镜像仓库存储限制，不代表全部这样的镜像都能成功启动为 Function。** [VCR limits](https://vercel.com/docs/container-registry/limits-and-pricing)

VCR 专项页只列 $0.10/GB 通用价格，主定价表则明确区分 Hobby 10 GB/月 included 与 Pro $0.10/GB-month。报告保留这一表述差异；真实验收应核对 Hobby 账号资格、Usage 和镜像储存累积，不能因“所有套餐可用”就省略费用检查。[VCR 专项页](https://vercel.com/docs/container-registry/limits-and-pricing)、[有套餐列的主定价表](https://vercel.com/pricing)

### 生命周期仍影响应用

Fluid Hobby 当前最大单次执行 300 秒，2 GB 内存、1 vCPU；网络等待计入请求时长。标准函数只读文件系统加临时空间，不应把实例文件或内存作为持久业务数据。持续不退出的队列 worker、依赖本地持久盘的数据库，不适合直接当成普通免费服务器运行。[Functions limits](https://vercel.com/docs/functions/limitations)、[文件系统与归档](https://vercel.com/docs/functions/runtimes#file-system-support)

WebSocket **现在支持**所有套餐 Beta，需 Fluid。连接到最大函数时长会断开，客户端需要重连、恢复订阅，重连也不保证落到同一实例；持久房间/状态需要外部存储。不能描述为“永久常驻 WebSocket 服务器”。[WebSockets，更新于 2026-08-10](https://vercel.com/docs/functions/websockets)

`waitUntil` 只延长请求生命周期内的后台工作，不等于无限运行的 daemon。Vercel 有独立 Workflows 方案支持持久化暂停与恢复；使用它意味着新增平台 API 与计费维度，本次不纳入 UpOK 通用容器能力。[Fluid background processing](https://vercel.com/docs/fluid-compute)、[Functions limits](https://vercel.com/docs/functions/limitations)

## 4. PostgreSQL 与开源赠额是不同的边界

Vercel Storage 当前主要为 Blob、Global Config 和 Marketplace 数据库；PostgreSQL 来自 Neon、Supabase 等提供方，套餐和额度由提供方决定。它可以把数据库凭证注入项目环境变量，但这不代表 Hobby 自动赠送一套无限 PostgreSQL，也不代表数据库归 UpOK 管理。[Storage overview](https://vercel.com/docs/storage)

**建议推论：** 首次 Vercel 验证继续使用独立的 Neon Free 账号/项目和标准 PostgreSQL 连接，先只改变计算平台，有利于比较迁移成本。是否走 Marketplace 自动开户另行评估，不在本轮同时更换数据库或创建资源。

Vercel 的 OSS Program 要申请并经选择，当前公开权益为 3 年共 $3,600 平台 credits；需持续维护、有影响或潜力、Code of Conduct，credits 仅用于该开源项目。Marketplace 服务商额度不包含在内。它是给获选项目的支持，不能变成所有 UpOK 用户免费额度的基础。[Open Source Program](https://vercel.com/open-source-program)

## 5. 下一次最小验证与待确认项

以下是研究建议，不是已完成的兼容性声明：

1. 用**用户自己账号中的个人非商用测试项目**，先只验证现有 Go HTTP + Neon 样例的 Vercel 路径；保留源端，不切换现有演示域名。
2. 优先试 `Dockerfile.vercel` 路径以延续语言无关契约，同时核对 Hobby 资格、Fluid 配置和 VCR 10 GB 额度是否在账号中实际可见。
3. 验收云端构建、读写 PG、版本更新、闲置后恢复、日志、核对原部署和数据保留；查看真实 CPU/内存/流量消耗再估算容量。
4. 若容器 Beta 兼容性或费用条件不满足，再比较标准 Go runtime；先实查其 Fluid 状态，不套用 Node/Python 额度。
5. 已有官方网页说明不足以证明我们的样例可部署、无需绑卡、可长期零费用或未来套餐不变；这些须在实际账号确认。

不以新增通用调度框架、自动跨云迁移、申请 OSS 补贴作为第一次验证的前置条件。

## 6. UpOK 的平台选择与实施顺序（建议）

保持“用户自己的账号、本地管理、普通多语言 HTTP 应用、费用需用户主动选择”的产品边界。按应用的运行要求和用途筛选可用方案，再比较免费额度。一个固定供应商无法同时覆盖个人展示、商业应用、常驻 worker 和所有容器依赖。

| 路径 | 最适合验证的场景 | 决定性边界 | 在 UpOK 中的建议角色 |
| --- | --- | --- | --- |
| Vercel Hobby + Neon | 个人非商用的 HTTP 应用，包含现有 Go/Rust 容器候选 | 用途限制、Container Images Beta、请求生命周期和多项月度额度 | 下一条优先做小型实测的计算路径 |
| Cloudflare 静态资源 / Workers + Neon | 静态前端，以及适配 Workers 的轻量 API | 动态请求每天 10 万、每次 10ms CPU、128MB；普通容器需 Paid | 适用应用的免费路线，保持运行时适配边界 |
| Railway + Neon | 已验证的普通容器部署、更新与故障核对 | 正式 Free 每月 $1；当前 UpOK 发布仅验收 Trial | 保留现有证据与适配，费用模式应明确展示 |
| Render Free + Neon | 接受明显冷启动、无持久本地文件的个人 HTTP 容器实验 | 每 workspace 750实例小时/月；15分钟空闲休眠，恢复约1分钟；费用状态和主动公网流量限制 | 容器备用候选，暂不同时实现 |

Vercel依据见上文；Cloudflare来源：[Workers limits](https://developers.cloudflare.com/workers/platform/limits/)、[静态资源计费](https://developers.cloudflare.com/workers/static-assets/billing-and-limitations/)、[Containers pricing](https://developers.cloudflare.com/containers/platform/pricing/)。Railway来源：[Plans](https://docs.railway.com/pricing/plans)、[Trial](https://docs.railway.com/pricing/free-trial)。Render来源：[Free](https://render.com/docs/free)、[Docker](https://render.com/docs/docker)。

Render 仅作候选：Hobby workspace 当前出站赠额5GB/月，绑有支付方式时可能产生额外流量费用；未绑支付方式则到限暂停。对外部数据库等主动公网访问的异常高流量，官方保留暂停免费服务的机制。因此不能只看“750小时”就替代费用与网络验收，也不把其30天到期的免费 PostgreSQL 当成长期数据库方案。[出站费用](https://render.com/docs/outbound-bandwidth)、[免费限制](https://render.com/docs/free)

### 先验证一条路径，再修改产品

1. **最高 ROI：Vercel Hobby 的资格与一次 Go 容器对照。** 复用已有样例和 PostgreSQL 数据模型，只改变计算部署方式；先确认账号能使用上述免费能力，再部署隔离测试项目。验证的是兼容性和可观测成本，不承诺整月零账单或普遍适用。
2. **验证通过后：新增一条可选 Vercel 路径。** 复用操作记录、授权、日志脱敏、访问检查和结果未知时的核对规则。当前 `Project`、资源检查和发布调用仍绑定 Railway/Neon；接入 Vercel 需要具体实现，不能仅靠换 CLI 或改配置宣称完成。
3. **再做独立用户复现。** 给出适用条件、安装入口及同一套可复现样例，观察注册、授权、部署、更新和诊断中的实际卡点。Cloudflare 按真实应用需要进入下一次验证。

第一步投入相对小，可同时回答“现有容器是否少改可用”“免费额度是否比Railway更适合”“Beta与供应商API是否足够接入”三个问题。一次性实现四个平台的投入大，且不能替代账号与应用实测，不建议作为下一步。

本次仅新增研究笔记。未调整既有ADR、代码或默认供应商，未登录Vercel、执行部署、改域名、申请付费或推送仓库。
