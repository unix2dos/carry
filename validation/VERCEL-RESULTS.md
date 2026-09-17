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

## Secret 边界与本地密钥管理

同日只读调用已有测试项目的环境变量接口，`DATABASE_URL` 和 `VALIDATION_TOKEN` 均为 `type: sensitive`、`visibility: secret`、作用于 `production`，响应不包含值。`PORT`、`APP_VERSION` 和 `DATABASE_SSLMODE` 为可读 Config。未修改变量，也未将凭证改为可读配置。

Vercel 官方说明 Secret 保存后只写不可读。因此当前 Railway 实现中依靠读回完整变量进行的数据库目标核对、已知密钥源码扫描和日志精确脱敏，不能原样移植。`/readyz` 成功只能证明应用自报数据库就绪，不能证明它连接的是登记的 Neon 数据库；供应商的构建日志遮蔽也不能代替 Ship 对所有运行日志的脱敏承诺。[Secret 官方说明](https://vercel.com/docs/environment-variables/sensitive-environment-variables)

用户确认采用本地密钥保存方案。已实现 macOS Keychain 存储、引用记录、保留旧值供日志遮蔽，以及源码上传前的已知密钥检查。合成值的真实钥匙串跨进程存取、不同构建读回和测试项清理通过。普通项目 JSON 不保存密钥值。本地保存与云端应用是分开的操作，缓存不能代替云端当前配置的证明。

Vercel 接入代码已加入本地分支，单元测试覆盖费用/归属限制、Secret 变更检测、未知写入与未知部署不重复提交。真实账号成功关联已有项目，但第一次 `DATABASE_URL` 同值 Secret 写入未能确认；其本地状态保持 `unknown`，尚未写入 `VALIDATION_TOKEN` 或发布新版本。

只读复查显示远端 Secret ID 和版本未变、写入标记没有出现，Secret 仍不可读；现有应用 `/healthz` 与 `/readyz` 返回200。原始 CLI 错误细节未留存，原因未确定。代码已对齐固定版本官方 CLI 使用的 v10 更新端点，并省略不需要修改的 key 字段；这只是待验证修正，未据此重试。按用户要求暂停云端变更，待讨论是否用独立的合成测试变量验证写入行为。[接入进展](results/vercel-adapter-report.json)

## 本地存储后续调整

用户随后选择受限权限的本地明文文件方案。当前代码已移除钥匙串/cgo 依赖，默认状态目录改为 `~/.ship`，业务密钥按项目单独保存。此前的钥匙串验证保留为历史证据。读取旧钥匙串的系统命令超时后，本机通过原始、已授权的私有测试输入恢复了缓存值；原钥匙串和旧状态保留回滚。存储迁移没有修改 Secret 写入标记、版本或 `unknown` 状态，也没有云端请求。[本地存储验收](results/local-secret-files-report.json)
