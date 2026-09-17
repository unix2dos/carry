# Vercel Hobby：Go 容器与 Neon 验证

2026-09-17。已在个人 Hobby 工作区完成 Go HTTP 容器的云端构建、PostgreSQL 读写、源码更新和数据保留检查。测试直接使用固定版本的官方 CLI，Ship 的正式发布命令尚未接入 Vercel。

[公开健康接口](https://upok-vercel-20260917-go.vercel.app/healthz) · [脱敏结果](results/vercel-container-report.json) · [工具与复现准备](vercel-tools/README.md)

## 结果

| 检查 | 实测结果 |
| --- | --- |
| 套餐与归属 | API 与网页确认个人 Hobby；原 Neon 组织仍为 Free；未升级套餐 |
| 镜像存储额度 | 账号 Usage 网页显示 10GB；实际 registry 有两份约31MB的镜像 |
| Docker 构建 | 日志确认多阶段 Go 编译、CA 证书安装和 VCR 推送 |
| v1 HTTP/PostgreSQL | 完整鉴权、输入检查和读写验证通过，0次传输重试 |
| 源码更新 | 源码加入版本标识后重新构建，镜像摘要改变，接口返回 `vercel-v1-source2` |
| 数据保留 | 新测试记录和原 Railway 验收记录在更新后都保持一致 |
| 闲置后访问 | 360秒无测试请求后，首次 `/readyz` 返回200，约1.780秒，TLS校验通过，0重试 |
| 日志 | 通过CLI读取17条请求日志，取样未发现已知数据库凭证或验证令牌 |
| 清理 | 删除本轮创建的临时数据行；保留测试项目、部署和镜像供复核 |

闲置检查没有取得平台已缩容到零的独立证据，因此不将1.780秒作为确定的容器冷启动指标或可用性承诺。

## 两个实际问题

### Other 框架设置导致 Ready，但应用未运行

第一次创建测试项目时指定了 `framework: null`，对应 Other。虽然上传了 `Dockerfile.vercel`，预检仍显示 Other；云端约30毫秒完成构建，没有容器构建记录，平台报 Ready，但 `/healthz` 返回404。

在样例 `vercel.json` 中显式设置 `{"framework":"container"}` 后，预检识别为 Container，后续日志确认真实Docker构建，业务检查通过。发布流程必须同时验证预设、构建产物和应用响应。

### 自动化 Chrome 曾被客户端拦截，手动访问已确认

匿名 curl 的实际HTTP与数据库检查通过，但工具控制的 Chrome 导航到同一健康接口时返回 `net::ERR_BLOCKED_BY_CLIENT`。没有修改浏览器扩展、代理或网络规则；具体拦截来源尚未确定。

后续只读诊断再次复现两次，Chrome 网络事件报告 `blockedReason: inspector`，未观察到主文档的服务端响应；同址 curl 返回200和正确版本。这把范围缩小到浏览器调试或控制路径，但还不能确定具体扩展或组件。浏览器工具的URL安全策略拒绝读取 `chrome://extensions/`，未尝试绕过。

用户随后手动在 Chrome 打开同一地址，报告能看到 `status: ok` 和版本字段的 JSON；用户没有逐字提供版本号。同轮 curl 复查返回200、TLS校验通过、版本为 `vercel-v1-source2`。普通浏览器访问已由用户确认，可继续评估适配器；工具控制路径没有重新验收，也未宣称已修复。[诊断记录](results/browser-access-diagnosis.json)

## 费用证据的边界

官方CLI的 `vercel usage` 在此Hobby账号上未返回计费数据，未将它当作用量为零。网页确认了实际套餐和各项额度，但新镜像已经出现在registry时，用量页面仍显示镜像存储为0，说明存在统计延迟。

这次证明了当前账号可以在Hobby条件下部署并运行样例，没有主动启用付费、创建新数据库或更换已有域名。它没有证明完整月份的零账单、全部账号资格、额度耗尽行为或商业用途可使用Hobby。

## 对 Ship 的含义

Vercel HTTP容器与外部PostgreSQL的组合已有实际证据，可继续评估为个人非商用应用的可选路径。原有20个本地样例/检查文件保持原始指纹；本轮只在隔离副本中添加Vercel配置和源码版本标识。

正式适配仍需实现Vercel的资源关联、费用预检、部署元数据核对与状态展示。其他语言、独立用户流程和提交响应丢失后的恢复需要分别验收。

## 适配前发现的 Secret 边界（待用户讨论）

同日只读调用已有测试项目的环境变量接口，`DATABASE_URL` 和 `VALIDATION_TOKEN` 均为 `type: sensitive`、`visibility: secret`、作用于 `production`，响应不包含值。`PORT`、`APP_VERSION` 和 `DATABASE_SSLMODE` 为可读 Config。未修改变量，也未将凭证改为可读配置。

Vercel 官方说明 Secret 保存后只写不可读。因此当前 Railway 实现中依靠读回完整变量进行的数据库目标核对、已知密钥源码扫描和日志精确脱敏，不能原样移植。`/readyz` 成功只能证明应用自报数据库就绪，不能证明它连接的是登记的 Neon 数据库；供应商的构建日志遮蔽也不能代替 Ship 对所有运行日志的脱敏承诺。[Secret 官方说明](https://vercel.com/docs/environment-variables/sensitive-environment-variables)

建议讨论的最小范围：保留 Secret；先实现已有计算项目的关联、费用与归属检查、部署、状态和中断核对。数据库分别展示资源归属与应用就绪证据，连接目标明确标为未核验；本版暂不通过 Ship 返回 Vercel 原始日志，用户在官方控制台查看。源码继续排除本地状态和敏感配置文件，但不得声称已与所有云端 Secret 值比对。是否接受这组能力边界，由用户确认后再实现。
