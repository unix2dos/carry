# 首版免费计算平台筛选

核对时间：2026-09-16。只读官方资料；未访问用户账号、创建资源或部署。文档支持候选筛选，不等于实际账号可用性和零账单验收。

后续补充：用户要求充分比较 Cloudflare，已完成 [Cloudflare 专项研究](2026-09-16-cloudflare-first-evaluation.md)。本文的“Railway 优先验证”是此前阶段建议，当前已提升 Cloudflare 计算路线的比较优先级，最终供应商仍未定案。

## 筛选条件

本地管理服务、用户自持账号、尽量少改现有 Web/API 代码、可连接外部 PostgreSQL、提供自动化接口、默认方案不在额度耗尽后自动收费。

## Railway Free：优先验证候选

- 当前 Free 为每月 $1 免费资源额度，区别于新账号一次性的 $5 / 30 天 Trial。Free 每服务最高 0.5 GB RAM，不能把资源上限当作整月可免费使用的量：[计划](https://docs.railway.com/pricing/plans)、[试用](https://docs.railway.com/pricing/free-trial)。
- 官方免费服务条款规定免费使用额度耗尽后继续使用需要升级付费服务；这支持把未升级的 Free 账号纳入不自动计费候选，但账号状态、额度耗尽后的具体恢复行为仍待实测：[服务条款](https://railway.com/legal/terms)。Hobby 有最低月费，不能当作 Free。
- 自动验证未通过的 Limited Trial 会限制出站网络和端口，因此跨平台数据库连接不是所有新账号都已获保证。工具必须识别此状态，验证连通性；不能自动通过付费升级绕过：[试用验证](https://docs.railway.com/pricing/free-trial)。
- 公共 GraphQL API 支持账号、workspace、project token 及 OAuth，可管理项目、服务、部署、变量等；Free API 限额为每小时 100 请求，需要避免高频轮询：[API](https://docs.railway.com/integrations/api)。
- Serverless 依据出站流量判断空闲，数据库连接池及后台网络活动可能阻止休眠。唤醒有冷启动，首个请求可能返回 502。设置修改在重新部署后作用于新容器：[Serverless](https://docs.railway.com/deployments/serverless)。
- 支持平台生成的服务域名；Free 的自定义域名等限制与 Trial 不同，应按最终 Free 能力验收：[定价比较](https://railway.com/pricing)。

判断：适合继续验证轻量、低流量 Web/API。不能承诺任意已有应用都能在每月 $1 内运行，不能只在 Trial 额度和权限下验收。

## Render Free：受账号计费状态约束的候选

- 支持常规语言和 Docker Web 服务。Free 为 0.1 CPU / 512 MB；15 分钟无活动后休眠，唤醒约一分钟；每 workspace 每月 750 免费实例小时，额度耗尽暂停。实例本地文件不持久：[Web 服务](https://render.com/docs/web-services)、[免费计划](https://render.com/docs/free)、[计算规格](https://render.com/docs/compute-plans)。
- 无支付方式时，出站额度耗尽停服、构建额度耗尽停止新构建；已绑定支付方式时可自动收取额外流量费和构建费。构建有 spend limit，本轮未找到同样覆盖出站流量的硬停用设置：[出站流量](https://render.com/docs/outbound-bandwidth)、[构建](https://render.com/docs/build-pipeline)、[FAQ](https://render.com/docs/faq)。
- 外部数据库访问可以使用，但异常高的服务主动公网流量可能触发暂停；阈值未公开。恢复可能要求付费升级：[免费限制](https://render.com/docs/free)。
- 官方创建服务 API 支持显式 `plan: free`；省略 plan 的默认值为付费规格。API 也可能返回要求支付信息的 402，不能保证所有新账号均能无卡创建：[API](https://api-docs.render.com/reference/create-service)、[OpenAPI 原文](https://api-docs.render.com/reference/create-service.md)。

判断：无支付方式且通过账号验证时可继续验证。已绑卡账号不能仅凭实例标记 Free 就纳入默认零收费路径。

## 其他已核对候选

- Cloud Run 适合普通 HTTP 容器，但付费计费账号超额自动计费，不作为当前硬性不自动收费约束下的默认方案：[定价](https://cloud.google.com/run/pricing)。
- Cloudflare Workers Free 可作为兼容应用的候选；必须先验证运行模型。Cloudflare Containers 属于 Workers Paid，不能借用 Workers Free 名义承诺普通容器零月费：[Workers 限制](https://developers.cloudflare.com/workers/platform/limits/)、[Containers 定价](https://developers.cloudflare.com/containers/platform/pricing/)。

## 实测之前不能作出的承诺

尚未验证新账号可开通、授权能覆盖必要 API、应用可构建启动、跨云数据库可连、冷启动可接受、免费额度到限和恢复行为、全过程未产生费用。首版只应承诺经过验证的组合和明确的账号条件。
