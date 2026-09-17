# 兼容性与迁移性：数据库、运行时与容器边界

核实日期：2026-09-16。

状态：第一方资料调查；没有连接用户数据库、执行迁移、部署服务或修改产品决策。

后续范围更新：Q24 用户明确普通 Go/Python/Node/Rust 等应用都重要，不采用本文较早提出的“先限 JS/TS”方案。当前建议转向标准 Linux 容器统一交付；文末已补充现有多语言构建能力，具体方案仍待确认。

## 要分开判断的三件事

本文采用三个独立问题来解释“可迁移”：代码是否能在目标运行时执行；数据及其语义是否能在目标数据库恢复；部署依赖的网络、存储与平台服务是否能重新配置。镜像可复制、数据库可导出或运行时开源，各自只证明其中一部分。以下官方事实支持这一拆分，最终产品取舍留待讨论。

## 1. PostgreSQL 有成熟的逻辑导出路径，但需要匹配工具和目标版本

`pg_dump` 是普通 PostgreSQL 客户端，可从能够连接源库且拥有相应读取权限的机器导出。SQL 文本 dump 由 `psql` 恢复；custom/directory 等归档由 `pg_restore` 恢复。逻辑导出可用于另一台机器或另一种硬件架构，不绑定某个托管商。[PostgreSQL SQL Dump](https://www.postgresql.org/docs/current/backup-dump.html)

版本需要分别核对源服务器、导出工具和目标服务器：`pg_dump` 不能导出比自身主版本更新的服务器；其输出预期支持导入更新版本 PostgreSQL，但**不保证可导入比导出工具主版本更旧的服务器**，即使源服务器恰好就是那个旧版本。不能只看两个供应商都写着 PostgreSQL。[pg_dump 文档](https://www.postgresql.org/docs/current/app-pgdump.html)、[PostgreSQL 官方文档源文件](https://github.com/postgres/postgres/blob/master/doc/src/sgml/ref/pg_dump.sgml)

**对迁移承诺的含义（推断）：** “托管 PG → 另一托管 PG / 本地 PG”是合理的能力范围，但应限定为已检查的版本与功能组合；“兼容 PostgreSQL”的其他引擎也需要实际恢复验证。

## 2. 业务数据之外，owners、roles、extensions 与 sequence 都会影响恢复

| 对象或条件 | 官方行为 | 迁移时的判断 |
|---|---|---|
| 业务表、schema、索引、约束、触发器等 | 完整导出包含数据定义与数据；表筛选不会自动补齐全部依赖 | 不能把“选几张表导出成功”当作应用 schema 已完整迁移 |
| Sequence | 数据导出包含 sequence 值；schema-only 不含这些值 | 恢复后须验证后续插入生成的键，不只比对现有行数 |
| Roles、tablespaces | 单库 `pg_dump` 不包含集群级角色与 tablespace 定义；`pg_dumpall` 能处理它们 | 托管商的集群权限通常不同，不能假定能照搬所有角色或存储路径 |
| Owner 与 ACL | 默认恢复原所有者及权限；目标角色不存在或权限不足会失败；可选择不恢复原 owner/ACL | 需要明确目标角色映射及权限结果，不能静默丢弃权限语义 |
| Extension | dump 主要记录 `CREATE EXTENSION`，不打包扩展程序的控制文件、SQL 脚本与本地库 | 目标必须支持并提供相应扩展，还要核对版本及启用权限 |

来源：[pg_dump](https://www.postgresql.org/docs/current/app-pgdump.html)、[SQL Dump：roles 与恢复](https://www.postgresql.org/docs/current/backup-dump.html)、[pg_restore](https://www.postgresql.org/docs/current/app-pgrestore.html)、[Extension Packaging](https://www.postgresql.org/docs/current/extend-extensions.html)、[扩展官方文档源文件](https://github.com/postgres/postgres/blob/master/doc/src/sgml/extend.sgml)。

`pg_restore` 默认可以在 SQL 出错后继续，并在结尾报告错误数；它还提供遇错退出与单事务恢复选项。因此“进程有输出、目标出现表”不足以证明迁移成功。具体恢复策略应依数据量、权限和目标能力确定，本文不提供生产执行脚本。[pg_restore](https://www.postgresql.org/docs/current/app-pgrestore.html)

## 3. 一致快照不是持续同步；保留源库也不自动等于随时无损回退

`pg_dump` 的表数据导出代表开始时的一致快照；正常情况下不会阻止源库继续读写。因此导出开始之后提交的新业务写入，不会自动追加入该 dump。[PostgreSQL SQL Dump](https://www.postgresql.org/docs/current/backup-dump.html)

**基于上述事实，对当前“允许停机”的范围可推导出这一流程：** 停止所有业务写入并等待进行中的事务结束 → 制作最终导出 → 恢复到目标 → 核对恢复错误、关键数据及业务读写 → 切换连接 → 恢复业务写入。这里的写入方包括后台任务与自动化，不只前端流量。该流程是迁移设计推论，不是已经实施的操作。

保留源库可让切换前的失败保有恢复路径；但目标一旦承接新写入，源与目标会出现差异。此后直接把连接串切回源库可能丢失新业务数据，必须另行处理差异。因而应分别定义“验证失败返回原服务”和“运行一段时间后的反向迁移”，不承诺两者成本相同。

## 4. D1 可导出；SQLite 类型转换与 D1 平台接口是两层问题

D1 官方支持将数据库的 schema 和数据导出为 `.sql`，并支持导入 SQLite 兼容 SQL。它并不是数据无法离开的封闭数据库。官方同时明确指出 PostgreSQL/MySQL 的类型与 SQL 语法不直接兼容，不能直接把它们的 dump 导入 D1。[D1 Import and export](https://developers.cloudflare.com/d1/best-practices/import-export-data/)

D1 导出有具体边界：包含虚拟表的数据库不支持正常导出；导出期间阻塞其他数据库请求；大整数经过 JavaScript 数字处理存在精度注意事项。这些都应由实际数据与迁移方法核验，不宜把“有导出命令”视为任意数据库无损迁移的证明。[D1 Import and export：known limitations](https://developers.cloudflare.com/d1/best-practices/import-export-data/#known-limitations)

另一个独立问题是应用使用 D1 的 Workers binding / HTTP API。即使数据移到了 SQLite，应用的数据库调用接口也可能需要适配；反过来，替换 API 接口并不会自动解决 PostgreSQL 与 SQLite 的类型、SQL 和事务行为差异。[D1 概览](https://developers.cloudflare.com/d1/)、[Workers bindings](https://developers.cloudflare.com/workers/runtime-apis/bindings/)

**判断：** 应表述为“SQLite 有迁移路径，但 PostgreSQL → D1 需要数据库语义转换与应用接口适配”。不能表述为“SQLite 不可迁移”，也不能把它纳入当前 PostgreSQL → PostgreSQL 的同一验收标准。

## 5. workerd 为离开 Cloudflare 运行 Workers 提供真实路径

Cloudflare 的 `workerd` 官方 README 明确将“自托管为 Cloudflare Workers 设计的应用”列为用途；提供本地启动配置与生产运行示例。它是 JavaScript/Wasm 运行时，官方仓库采用 Apache-2.0 许可证，并使用 compatibility date 保持 API 行为兼容。因此“Workers 代码绝对无法离开 Cloudflare”不符合官方事实。[workerd README](https://github.com/cloudflare/workerd)、[LICENSE](https://github.com/cloudflare/workerd/blob/main/LICENSE)

但自托管范围仍需具体核对：运行时二进制有 OS、CPU 与系统库要求；平台配置使用独立的配置与 bindings，不能把可运行的脚本文件等同于完整部署。本文没有安装 workerd 或做应用运行验证。[workerd README](https://github.com/cloudflare/workerd)

## 6. 开源运行时不等于托管 D1/R2/Durable Objects 的数据与服务一起迁走

这不是说 workerd 完全没有这些能力。当前官方配置源码实际支持 Durable Object namespace、SQLite 存储 API，以及内存或本地磁盘的存储后端；本地磁盘配置仍标为 experimental。R2 binding 则把操作转成发向指定 service 的 HTTP 请求。[workerd 配置源码](https://github.com/cloudflare/workerd/blob/main/src/workerd/server/workerd.capnp)

**能证明的范围：** 运行时代码、部分 API 与可配置存储后端存在自托管基础。

**不能据此证明的范围：** Cloudflare 线上 D1、R2、DO 数据能自动导入这些后端；云端复制、备份、网络路由与可用性保障也会随 runtime 一起出现。这些需要逐项导出数据、配置或实现相应服务、验证语义并承担运维。该区别来自 README 与配置源码的能力边界，而不是“开源等于没有锁定”或“用了 binding 就永远无法迁走”的二选一。

## 7. Docker 镜像提高打包可移植性，但目标运行环境仍有合同

Docker 官方明确指出，容器共享主机内核，单个平台镜像仍受 OS 和 CPU 架构约束；`linux/amd64` 在 ARM 上通常需要适配的镜像变体或仿真，Windows 容器也不能直接放到 Linux 内核运行。多平台镜像通过各平台变体解决这部分问题。[Docker multi-platform builds](https://docs.docker.com/build/building/multi-platform/)

运行状态与网络也在镜像之外：volume 生命周期独立于容器，需要单独备份、恢复或迁移；容器端口能否从外部访问取决于端口发布、网络与主机配置。镜像不会自动携带原供应商的磁盘数据、可达的数据库地址或网络规则。[Docker volumes](https://docs.docker.com/engine/storage/volumes/)、[Docker port publishing](https://docs.docker.com/engine/network/port-publishing/)

**对“OCI/Docker 可移植”的准确解读（推断）：** 标准镜像减少重打包与依赖安装的差异，但真正验收还应覆盖启动方式、OS/架构、端口、CPU/内存、持久状态、依赖服务与网络可达性。它是一种有用的基础，不是“任意云零改动运行”的保证。

## 尚未实测

- 代表性业务库导出后的完整恢复、权限映射、sequence 后续写入以及所需扩展。
- 源、目标版本及目标托管服务限制；本轮未连接任何用户数据库。
- 某个真实 Workers 应用在 workerd 上运行，以及其 bindings 与数据后端的替换。
- 某个真实 Docker 应用在所选平台的架构、状态与网络限制下运行。

## 对本项目的建议（分析，尚未定案）

### 把接入与迁出分开验收

用户强调兼容性和迁移性后，不宜继续仅按供应商免费额度排列候选。应先判断应用的运行要求与目标是否相容，再明确退出路径，并检查免费账号与费用条件。默认不自动收费的约束仍然有效：如果某个现有项目没有满足这些条件的免费方案，应明确报告缺口，而不是静默改变语言、数据库或收费模式。

| 要证明的能力 | 可检查的结果 | 单独不足以证明的东西 |
| --- | --- | --- |
| 接入已有应用 | 必需功能能运行，适配改动可见，限制明确 | 只完成构建或拿到部署 URL |
| 迁出计算平台 | 同一业务代码能在另一运行环境恢复功能 | 只导出源码或复制镜像 |
| 迁出数据库 | 目标恢复所需结构与数据，权限和后续写入正常 | dump 命令退出成功或行数看似一致 |
| 离开本工具 | 用户能使用保留的配置、资源标识和官方工具继续管理 | 工具关闭后应用暂时还在运行 |

### 首版应用基线建议

先限定为无状态 HTTP Web/API 与 PostgreSQL 业务数据，并保留用户代码的正常运行路径。运行时、框架版本、系统依赖、持久状态和后台任务应有明确支持范围。此处是建议，用户没有确认首版语言范围。

若首批选择 JS/TS HTTP 应用，可以验证 Workers 部署与普通 Node/Docker 运行两条路径。平台入口、构建和数据库连接配置允许不同，核心业务逻辑与数据模型保持一致。只把本地模拟 Workers 跑通不算计算迁出证明；如果原生 Node 路径不可行，也可单独评估 workerd 自托管，但不能自动声称已经独立于全部 Cloudflare 服务。

若首版必须支持 Go/Python 等普通进程或较多原生依赖，容器路径更值得优先验证，Workers 仅用于实际兼容的项目。容器也需要验证目标架构、端口、资源和状态约束。无需为了证明多云而首版实现所有供应商。

### 标准接口有帮助，但不抹掉差异

优先复用应用已有的 HTTP 入口、启动方式、配置和 PostgreSQL 驱动；不要要求业务代码引入本项目专有 SDK 才能运行。平台相关代码尽量集中在确实需要的入口或部署配置中。等两条真实路径暴露差异后再抽取适配逻辑，不先构建覆盖所有云的统一资源框架。

数据库选型还要看应用使用了什么能力。只连接托管 PostgreSQL，与额外依赖某供应商认证、存储或实时服务的应用，迁移范围不同。标准 SQL、容器、开源运行时都能降低某一部分成本，但无法替代完整迁移验证。

### 最小迁出验收建议

1. 一个公开示例先按普通方式在本地运行，使用 PostgreSQL 业务表。
2. 把它部署到选定计算平台与托管 PostgreSQL；记录入口、构建与配置差异，验证业务功能。
3. 将同一业务代码在普通 Node/Docker 等已选定的另一运行环境恢复；本地证明不等于第二家云的网络、域名与费用已验证。
4. 按 Q22 停写并把业务数据恢复到兼容 PostgreSQL，检查关键记录、约束、权限和 sequence；验证产生的测试写入应与真实业务写入区分，必要时在可回滚事务中进行。
5. 在目标开放真实业务写入前确定切换；开放之后的回退需要处理新增数据，不能直接宣称保留源库就可无损返回。
6. 用户保留源码、构建配置、数据备份、资源标识与官方管理步骤。密钥由用户单独安全保管，不明文放进普通导出包。

以上未实施，也尚未全部获得用户确认。它们用于把“兼容、可迁移、不锁定”转成可以公开展示的证据。

### 对当前候选的影响

- Workers + 外部 PostgreSQL 仍值得验证，适用于具体兼容的应用；首版默认平台尚未确定。
- Workers + D1 适合本来就采用 SQLite 语义且愿意使用其 API 的应用，不能因免费而自动替换已有 PostgreSQL 模型。
- 普通容器 + PostgreSQL 是重要的运行与迁移参照；不据此承诺任意云、任意应用都能零改动运行。
- 当前最有决定性的用户选择，是首批被部署应用的范围，而非管理工具自身用什么语言编写。

## 多语言兼容补充：镜像与自动构建

核实日期：2026-09-16；以下是官方能力说明，未执行真实构建或部署。

1. **镜像统一交付形式，语言仍决定内部构建。** Dockerfile 可用不同构建阶段编译代码，再把产物复制进最终运行镜像；Go/Rust 的二进制与 Python/Node 的程序、运行时及依赖可以分别打包。部署平台接收镜像，不必自行实现每种语言的编译器。[Docker multi-stage builds](https://docs.docker.com/build/building/multi-stage/)
2. **镜像不是任意工作负载通行证。** OS/CPU 架构仍需匹配，持久数据要独立管理；特权、GPU、网络及有状态需求必须再核对目标能力。多语言普通 HTTP 服务与任意系统级程序是不同承诺。[Multi-platform builds](https://docs.docker.com/build/building/multi-platform/)、[Volumes](https://docs.docker.com/engine/storage/volumes/)、[GPU access](https://docs.docker.com/engine/containers/gpu/)
3. **Railpack 当前明确支持 Node、Python、Go、Rust。** 官方流程是检测源码、安装运行时与依赖、执行构建并产出容器镜像；支持列表还包含其他语言。支持某语言不等于所有框架、原生依赖或仓库布局都能零配置通过。[Railpack](https://docs.railway.com/builds/railpack)、[支持列表](https://railpack.com/getting-started)
4. **已有构建配置可以优先保留。** Railway 会检测并使用源码根目录的 `Dockerfile`，支持自定义路径；这是 Railway 构建入口规则，不是 Railpack 自身保证。使用 Railpack 时可通过 `railpack.json` 自定义步骤及启动命令。[Dockerfiles](https://docs.railway.com/builds/dockerfiles)、[Railpack configuration](https://railpack.com/config/file)
5. **自动构建不是平台必须自研的语言框架。** Cloud Native Buildpacks 的语言覆盖取决于 builder 所含 buildpacks，检测不匹配可以失败。基于这些事实，可考虑先接已有镜像/Dockerfile，缺失时复用自动构建或由用户已有 Agent 辅助生成配置；最终通过实际构建、启动与 HTTP/数据库功能检查确认支持。这是建议推论，尚未验收。[CNB builder](https://buildpacks.io/docs/for-app-developers/concepts/builder/)

## 多语言范围下的当前建议

平台区分两层能力：一是接收已验证的镜像、启动服务并管理生命周期；二是帮助用户把源码构建成镜像。第一层可以不设语言白名单，第二层按已有 Dockerfile、成熟构建工具及 Agent 辅助逐步完善，实际能力仍要通过项目验证。

建议首版继续聚焦 HTTP Web/API；语言广泛不意味着 GPU、特权容器、任意持久盘或复杂集群也自动纳入同一承诺。应用需要满足目标的 OS/架构、端口、资源和状态要求。先用 Go/Python/Node/Rust 小型样例跑相同验收流程，提供公开证据。

Cloudflare Workers 官方确有 Rust 支持，通过 workers-rs 等工具编译到 Wasm 并接入其请求与 binding 模型；这与原生 Linux Rust 服务的兼容性应分别判断。需要普通容器执行方式时，Cloudflare Containers 是付费选项，不能自动承担默认零收费的通用计算路径。[Workers Rust](https://developers.cloudflare.com/workers/languages/rust/)、[Containers](https://developers.cloudflare.com/containers/)

## 研究方法与范围

使用 Agent Reach 的 Jina Reader 读取第一方文档，以 GitHub CLI 只读读取 PostgreSQL 和 Cloudflare 官方仓库作为交叉证据，并以 web 核对 Docker 文档。未调查免费额度，未访问账号或凭证，未创建代码或执行部署。本文为事实与推论记录，不是生产迁移操作手册。
