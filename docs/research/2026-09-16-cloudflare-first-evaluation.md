# Cloudflare 作为首版默认平台：免费额度、数据库和费用边界

核实日期：2026-09-16。

状态：官方资料研究，尚未部署或用真实账号验证；没有修改产品决策。

## 本次问题与边界

评估 Cloudflare 是否值得替代当前讨论中的跨平台首版候选。既有目标是本地管理 server 与 Web UI、接入用户已有编码 Agent、用户持有云账号、普通现有 Web/API 尽量少改代码、默认不自动收费，只有用户主动选择才启用付费能力。首版已确认的业务表迁移范围是 PostgreSQL 到兼容 PostgreSQL，可停机并保留源库。

本文核实 Cloudflare 的免费额度、数据库性质、收费边界和现有应用兼容性，并在文末比较三条候选路线。供应商与首版兼容范围尚未定案。

## 已确认的核心结论

1. **Workers Free + 静态资源 + D1 有真实免费路径。** Workers 与 D1 产品页均明确提供免信用卡起步；Workers Free 请求和 D1 日读写额度用尽后有报错规则，不会仅因达到这些免费额度而自动变成付费套餐。此结论限定在相应产品的 Free 计划，不能外推到 R2、Containers 或已订阅 Workers Paid 的账号。[Workers 产品页](https://www.cloudflare.com/products/workers/)、[D1 产品页](https://www.cloudflare.com/products/d1/)、[Workers 限制](https://developers.cloudflare.com/workers/platform/limits/)、[D1 定价](https://developers.cloudflare.com/d1/platform/pricing/)
2. **D1 的 SQL 语义是 SQLite，不能作为已确认的 PostgreSQL 兼容迁移目标。** Hyperdrive 可以连接外部 PostgreSQL，但它提供连接池与缓存，数据库仍由原供应商托管；这条路线仍是跨平台组合。[D1 概览](https://developers.cloudflare.com/d1/)、[Hyperdrive 概览](https://developers.cloudflare.com/hyperdrive/)
3. **Cloudflare Containers 没有 Workers Free 额度。** 它要求 Workers Paid，最低账户订阅费 $5/月，再按 Containers 以及关联 Workers、Durable Objects 的实际用量计费。[Containers 定价](https://developers.cloudflare.com/containers/platform/pricing/)
4. **R2 是带免费额度的用量计费订阅，不是免费额度耗尽就停止的产品。** 启用需完成 checkout；超额计入账单，并自动向账户支付方式收款。不能把 R2 自动纳入“默认不收费”的部署流程。[R2 开通](https://developers.cloudflare.com/r2/get-started/)、[R2 定价](https://developers.cloudflare.com/r2/pricing/)、[账单规则](https://developers.cloudflare.com/billing/understand/how-billing-works/)

## 免费额度和触顶行为

| 产品 | 当前免费额度或起点 | 到限行为与限制 |
|---|---|---|
| Workers Free | 账户每天 100,000 动态请求；每次 10 ms CPU；128 MB 内存 | 日请求额度在 UTC 午夜重置，超额发生 1027；代理路由还受 fail-open/fail-closed 设置影响。10 ms 是 CPU 时间，不是网络等待总时长。 |
| Workers Static Assets | 静态资源请求免费且不限请求数；资产存储不额外收费 | 免费每 Worker 版本最多 20,000 文件，单文件最大 25 MiB；若配置先运行 Worker，则请求进入动态调用额度。 |
| Pages | 静态资源请求免费且不限请求数；Free 每月 500 次构建 | 每次构建最长 20 分钟、Free 一次只运行一个构建；Pages Functions 与 Workers 共享每天 100,000 动态请求，不是另领一份额度。 |
| D1 Free | 每账户最多 10 库、总存储 5 GB；单库最多 500 MB；每天读 500 万行、写 10 万行 | 达到日读写限制后查询报错；UTC 午夜重置。存储触顶后需清理空间才能继续插入数据或创建、修改表等对象。 |
| Hyperdrive Free | 每天 100,000 条数据库语句 | 包含 SELECT、INSERT/UPDATE/DELETE、DDL，以及命中缓存的查询；超额报错，UTC 午夜重置。外部数据库的费用独立。 |
| Containers | Free 不可用；Workers Paid $5/月起 | 包含部分 CPU、内存、磁盘用量，超额计费；不是零费用容器入口。 |
| R2 Standard | 每月 10 GB-month、100 万 Class A 操作、1,000 万 Class B 操作；直接出站流量不收费 | 超过免费额度后按量计费；Infrequent Access 不享用这些免费额度。 |

来源：[Workers 定价](https://developers.cloudflare.com/workers/platform/pricing/)、[Workers 限制](https://developers.cloudflare.com/workers/platform/limits/)、[Static Assets](https://developers.cloudflare.com/workers/static-assets/billing-and-limitations/)、[Pages Functions 定价](https://developers.cloudflare.com/pages/functions/pricing/)、[Pages 限制](https://developers.cloudflare.com/pages/platform/limits/)、[D1 限制](https://developers.cloudflare.com/d1/platform/limits/)、[D1 定价](https://developers.cloudflare.com/d1/platform/pricing/)、[Hyperdrive 定价](https://developers.cloudflare.com/hyperdrive/platform/pricing/)、[Containers 定价](https://developers.cloudflare.com/containers/platform/pricing/)、[R2 定价](https://developers.cloudflare.com/r2/pricing/)。

### D1 的两个容易混淆之处

- **5 GB 是账号所有数据库合计，单个免费数据库只有 500 MB。** 不能把它当作一个免费 5 GB PostgreSQL 实例。D1 Paid 单库最大 10 GB，这个单库限制也不能申请再提高。[D1 限制](https://developers.cloudflare.com/d1/platform/limits/)
- **行读取量不是返回行数，也不是查询次数。** 扫描 5,000 行、最终返回 10 行的查询可能计 5,000 行读取。索引更新也会增加写入行数；迁移导入同样消耗读写额度。[D1 定价说明](https://developers.cloudflare.com/d1/platform/pricing/)

Cloudflare 在 **2026-09-01** 发布了日额度强制执行公告：Free 账号超出 D1 日读写额度后，Binding API 与 REST API 都会报错直到 UTC 午夜，已保存数据不受影响。这是当前日期之前刚生效的边界，不能沿用早期额度未强制执行时的经验。[2026-09-01 公告](https://developers.cloudflare.com/changelog/post/2026-09-01-d1-free-tier-limit-enforcement/)

## 套餐与支付方式改变了什么

### Workers、D1 与 Hyperdrive

Workers 和 D1 官方产品页明确写可以无信用卡起步；Workers 文档也说明默认提供 Free 计划。这支持无卡免费路径，但本轮未实测新账号注册、地域资格或风控流程。[Workers 产品页](https://www.cloudflare.com/products/workers/)、[D1 产品页](https://www.cloudflare.com/products/d1/)、[Workers 定价](https://developers.cloudflare.com/workers/platform/pricing/)

**D1 按账号的 Workers Free/Paid 计划适用不同规则。** 升级 Workers Paid 后，D1 日硬额度改为每月包含 250 亿行读取、5,000 万行写入及 5 GB 存储，超过后分别按 $0.001/百万行读取、$1/百万行写入和 $0.75/GB-month 计费。Hyperdrive 则随 Workers Paid 取消每天 100,000 语句限制，连接池和查询缓存无额外 Hyperdrive 费用，但外部数据库仍独立计费。[D1 定价](https://developers.cloudflare.com/d1/platform/pricing/)、[Hyperdrive 定价](https://developers.cloudflare.com/hyperdrive/platform/pricing/)

**由此推断：** 若用户账号已因其他项目订阅 Workers Paid，在该账号上新建本项目的 D1，不能仅靠“本工具未主动升级”就宣称到限自动停止。需要读取实际账号套餐，并把当前适用费用模式纳入部署前展示。这是产品需求推论，不是已实现能力。

### Containers

Workers Paid 每月包含 Containers 的 25 GiB-hours 内存、375 vCPU-minutes 与 200 GB-hours 磁盘；额外用量分别按 $0.0000025/GiB-second、$0.000020/vCPU-second、$0.00000007/GB-second 计费。内存和磁盘按所选实例的已配置量，CPU 按活跃使用量；睡眠后停止计算相关运行费用。网络出站有独立额度和区域费率，入口 Workers、对应 Durable Objects、日志也适用各自计费。[Containers 定价](https://developers.cloudflare.com/containers/platform/pricing/)

**由此推断：** “支持 Docker/普通容器”不能直接转述成“能免费接收现有 Docker 应用”。兼容能力和订阅门槛需分别判断。

### R2

R2 需要独立订阅，官方开通文档要求在控制台完成 checkout。计费政策要求启用订阅前有有效支付方式，且明确描述 R2 可能接受支付方式预授权；支付验证失败可能暂停 R2。这里的要求是支付方式，不应一律写成只接受信用卡。[R2 开通](https://developers.cloudflare.com/r2/get-started/)、[Billing policy](https://developers.cloudflare.com/billing/understand/billing-policy/)

R2 Standard 超过免费量后，存储 $0.015/GB-month，Class A $4.50/百万次，Class B $0.36/百万次；按官方规则向上舍入计费单位。账单按量结算，并可在达到累计用量费用阈值时提前自动收款。这里的“阈值”是收款触发值，不是预算熔断值。[R2 定价](https://developers.cloudflare.com/r2/pricing/)、[账单规则](https://developers.cloudflare.com/billing/understand/how-billing-works/)、[Threshold billing](https://developers.cloudflare.com/billing/threshold-billing/)

**本轮证据不支持 R2 有“免费额度耗尽自动拒绝、永不产生费用”的默认模式。** 也未实测具体账号和地区的 checkout、支付方式选择或是否有其他特殊资格。不要用删除支付方式或支付失败模拟预算上限。

## 数据库产品边界

D1 是托管数据库，使用 SQLite SQL 语义，并提供 Workers binding 和 HTTP API。它不是 PostgreSQL wire protocol 的替代终点。[D1 概览](https://developers.cloudflare.com/d1/)

Hyperdrive 管理对既有 PostgreSQL/MySQL 数据库的连接池与查询缓存，可连接 Neon 等供应商；它不承担源数据库的存储托管。Cloudflare 控制台还可以创建 PlanetScale 数据库并把供应商费用合并到账单，但 PlanetScale 用量与 Hyperdrive 用量分开收费，因此这不构成 Cloudflare 自带免费 PostgreSQL 的证据。[Hyperdrive 概览](https://developers.cloudflare.com/hyperdrive/)、[Hyperdrive 定价](https://developers.cloudflare.com/hyperdrive/platform/pricing/)

**对既有范围的影响（推断）：** 选择 Workers + D1 需要重新讨论 PostgreSQL 兼容迁移承诺；选择 Workers + Hyperdrive + 外部 PostgreSQL 可以保留数据库产品类型，但仍有多个供应商。本文不据此修改已确认方向。

## 待实测与尚未证明

- 新 Free 账号从注册、授权到创建 Workers/D1 的实际流程；已有 Workers Paid 账号应如何明确选择费用模式。
- 代表性应用在 Workers Free 上的 CPU、内存、依赖兼容和实际请求表现；不能把产品营销中的通用语言支持当作现有应用零改动保证。
- D1 免费读写日额度在应用真实 SQL、索引和导入流程下的消耗；不会主动压满真实账户额度来验证。
- Hyperdrive 与所选 PostgreSQL 提供商、驱动、事务及迁移工具的完整链路；数据库提供商额度仍需单独核对。
- R2 checkout 和账户付费授权的具体呈现，以及是否能通过官方机制设置所需硬上限。本轮仅证实按量计费和自动收款，没有证实硬上限。

## 运行兼容性：比纯函数更广，仍需按应用验证

### Node.js 与 Web 框架

Workers 当前提供相当一部分 Node.js API；官方文档说明 `2026-08-04` 及之后的兼容日期默认启用 Node.js 兼容功能。不能继续笼统地说 Workers 无法接入已有 Node.js 服务，但官方同样列出部分 API 与非功能性 stub，包括 `child_process`、`cluster` 等；可以导入模块不等于对应系统能力可用。[Node.js 兼容](https://developers.cloudflare.com/workers/runtime-apis/nodejs/)

`httpServerHandler` 可以把 Node.js `http.createServer` 处理器接入 Worker 请求模型，已有路由逻辑可能复用。`listen()` 的端口在这种模式下是逻辑路由标识，并非用户拥有了一台可任意监听端口的服务器。[Node HTTP](https://developers.cloudflare.com/workers/runtime-apis/nodejs/http/)

Next.js 有官方接入指南，当前文档介绍 vinext，并保留 OpenNext 等其他路径。保留应用目录与路由不等于部署工具链、所有依赖及所有框架特性完全不变，仍须对所选版本和功能验收。[Next.js 指南](https://developers.cloudflare.com/workers/framework-guides/web-apps/nextjs/)

### Python 与普通容器

FastAPI 有官方 ASGI 接入路径，示例通过 `workers.asgi.entrypoint(app)` 暴露既有应用。Python Workers 使用 Pyodide/WebAssembly，标准库、依赖包和系统能力有适配范围，不能当成任意 Linux CPython 进程。[FastAPI](https://developers.cloudflare.com/workers/languages/python/packages/fastapi/)、[Python 标准库](https://developers.cloudflare.com/workers/languages/python/stdlib/)、[依赖包](https://developers.cloudflare.com/workers/languages/python/packages/)

**由此推断：** Workers 的“少改代码”应按项目判断。静态前端、轻量 JS/TS API 适合优先验证；Node.js 服务与 FastAPI 需要检查入口和依赖。普通 Go 可执行文件、已有 Docker 镜像或需要系统子进程的应用，不能直接视为免费的 Worker 部署单元。保持普通容器执行方式时应考察 Containers 或其他容器平台，而 Containers 的订阅门槛已在前文列明。[Containers 概览](https://developers.cloudflare.com/containers/)

### PostgreSQL 与 D1 不是换连接串的关系

Cloudflare 明确说明 PostgreSQL/MySQL 的 SQL dump 不能直接导入 D1，因为类型和语法不兼容。可进行专门的结构与数据转换，但这属于新的迁移工作，不能冒充已经确认的 PostgreSQL 到兼容 PostgreSQL 迁移。ORM 的不同数据库适配器也不自动消除查询、类型、扩展和事务使用差异。[D1 导入导出](https://developers.cloudflare.com/d1/best-practices/import-export-data/)

Workers 可以连接外部 Neon/Supabase PostgreSQL，经 Hyperdrive 与原生驱动，或供应商的 HTTP 驱动等路径。Hyperdrive 是否必要及驱动、事务、延迟和 Free CPU 预算是否满足，需要通过代表性应用验证。[数据库连接](https://developers.cloudflare.com/workers/databases/connecting-to-databases/)、[Neon 接入](https://developers.cloudflare.com/workers/databases/third-party-integrations/neon/)

## 对首版路线的建议（分析，尚未获得用户确认）

| 候选 | 优势 | 需要接受或验证的边界 |
| --- | --- | --- |
| Workers Free + D1 Free | 一个资源账号内完成计算和 SQL 数据库，日额度清楚，适合从零搭建轻量应用 | 使用 SQLite；不能直接满足 PostgreSQL 兼容迁移，首版也未验证跨供应商组合；需用户重新决定这两项取舍 |
| Workers Free + Neon Free（已有 Supabase 可外接） | Cloudflare 计算与静态资源，同时保留 PostgreSQL 和跨平台目标 | 两个供应商；Workers 应用、驱动、CPU/内存预算与双侧额度须验证 |
| 普通容器平台 + PostgreSQL | 对既有普通服务的运行模型更接近 | 免费计算通常有休眠、额度、账号资格或计费状态限制；不能保证任意现有应用免费运行 |

**当前建议：把 Cloudflare 提升为优先验证的计算平台，先讨论是否以 Workers + 外部 PostgreSQL 收敛首版。** 这一组合最接近已确认的 PostgreSQL 迁移与跨平台目标；它并不预先保证每个现有项目都能运行。如果用户更重视一个账号完成全部免费服务，Workers + D1 是合理替代，需要明确调整数据库和首版跨平台要求。

Cloudflare 请求数、D1 行数与 Railway 的算力抵扣额是不同单位，不能按数值直接排名。对轻量、间歇访问的 Web/API，Workers + D1 的长期免费入口和日额度具有吸引力；CPU 密集、依赖系统能力或原生 PostgreSQL 的应用需要不同评估。这是基于产品能力作出的判断，不是已完成性能或实际账单对比。

### 建议的最小验证范围

1. 明确 Workers Free / Paid 账号状态及所选数据库计划，验证所需授权；不通过自动升级绕过资格或额度。
2. 只选一条数据库路线，用公开示例验证构建、发布、业务读写及再次更新。
3. 记录真实请求的 CPU、内存及数据库操作消耗，测试错误与恢复，不故意耗尽整个真实免费账号额度。
4. 使用已有确认的 PostgreSQL 迁移验收，或在用户选择 D1 后重新明确迁移目标与转换范围。
5. 默认不启用 R2、Containers 等可能自动计费的附加订阅；需要时另行展示条件并由用户选择。

这些是后续验证建议，本轮没有执行。

## 研究方法

使用 Agent Reach 的 Jina Reader 路由读取 Cloudflare 第一方文档；以官方 Markdown 页面及 web 搜索/打开交叉核实。没有使用第三方文章作为结论依据，没有访问用户云账户、调用资源创建接口、保存凭证或试部署。费用和额度是核实日期的规则，实施前应重新检查官方页面。
