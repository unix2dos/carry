# 个人云验证：登录与只读预检

固定官方 CLI：Railway `5.57.2`、Neon `4.18.0`。工具安装在本目录，未全局安装，也未安装供应商的 Agent skills/MCP。

尚未注册时，先在官方网页完成个人账号注册，再运行下方 CLI 登录命令。本次实测 Railway 等待回调 5 分钟、Neon 等待 60 秒；注册耗时可能超过授权窗口，超时后重新运行登录命令即可。

## 1. Railway 个人注册 / 登录

```sh
cd validation/cloud-tools
umask 077
env -u RAILWAY_TOKEN -u RAILWAY_API_TOKEN ./node_modules/.bin/railway login
```

浏览器会打开官方登录授权页面，未注册时进入注册流程。用户选择自己的个人测试身份并完成授权，不通过升级付费来绕过资格验证。CLI 等待浏览器回调；过期后重新运行同一命令即可。

Railway 官方 CLI 使用 `~/.railway/config.json` 保存登录信息，项目私有安装不会隔离该账号状态。本次开始前只检查了该文件是否存在，没有读取凭证；当时不存在。不要把该文件或其内容复制进项目或聊天。

授权后只读检查：

```sh
env -u RAILWAY_TOKEN -u RAILWAY_API_TOKEN ./node_modules/.bin/railway whoami --json
```

根据返回身份与 workspace 选择本次个人测试目标，再检查套餐、用量和网络资格。`whoami` 不证明免费资格。尚未选择目标前，不执行 `init`、`add`、`up`、付费设置或其他资源变更。

## 2. Neon 个人注册 / 登录

本次使用独立的私有认证目录，避免混用默认账号配置：

```sh
cd validation/cloud-tools
umask 077
mkdir -p "$HOME/.config/neon"
chmod 700 "$HOME/.config/neon"
env -u NEON_API_KEY -u NEON_PROFILE ./node_modules/.bin/neon login \
  --config-dir "$HOME/.config/neon" --no-analytics
```

用户在官方浏览器页面选择个人账号并授权。凭证保存在上述本机私有目录，不写入仓库；无需把 token 粘贴到聊天。此命令仅做认证，不创建测试项目。

授权后只读检查：

```sh
env -u NEON_API_KEY -u NEON_PROFILE ./node_modules/.bin/neon me --output json \
  --config-dir "$HOME/.config/neon" --no-analytics
env -u NEON_API_KEY -u NEON_PROFILE ./node_modules/.bin/neon orgs list --output json \
  --config-dir "$HOME/.config/neon" --no-analytics
```

选定个人组织后，再读取该组织的 plan；不要用登录成功、账户用量为零或“新注册”推断长期免费资格。

## 3. 进入资源创建前必须记录的事实

- 用户明确选择的个人身份和 Railway workspace / Neon organization。
- Railway 的 Free/Trial/Paid 计划及 Full/Limited Trial 网络资格。新账号可能首先处于 Trial；Trial 验证不能标成正式 Free 验收。
- Neon 目标组织的 Free 计划及相应权限。
- 允许的构建、计算、网络用量和超额行为，不自动启用付费。
- 即将创建的隔离测试资源与样例范围，不能混入公司或已有业务资源。

认证与只读查询都不等于这些条件已经通过。2026-09-16 的真实核查确认 Neon 个人组织为 Free，Railway 为已验证的 Trial、无付费订阅或默认支付方式；随后已创建隔离测试资源。实际进展见 [云端验证结果](../CLOUD-RESULTS.md)。

## Railway 服务配置

当前新建服务不能再采用 `railway.json` / `railway.toml` 的 Config as Code。已有服务的兼容支持也将在 2026-12-01 截止，见 [官方迁移说明](https://docs.railway.com/guides/config-as-code)。本次通过真实部署元数据发现旧配置没有生效后，改用官方 API。

`railway-settings.json` 保存此次验证的 API 参数，不是 Railway 自动加载的配置文件。`service` 传给 `serviceInstanceUpdate`，`limits` 补上目标 service/environment ID 后传给 `serviceInstanceLimitsUpdate`。执行前检查实际 CLI schema；提交后读取 `serviceInstanceLimits` 并核对最终部署的 `meta.serviceManifest`。不把 API 返回成功等同于新实例已运行。

Railway API 的 `plan=HOBBY` 本身也不能证明是付费账号。本次结合 `customer.isTrialing=true`、无付费订阅、无默认支付方式与网页的 Trial 标识判断费用模式。

## 远程 HTTP 检查的传输备用方式

如果本机 Python urllib 的 TLS 连接不稳定，可以让已安装的 curl 执行同一组检查：

```sh
python3 validation/cloud-tools/curl-probe.py \
  --url https://your-fixture.example --language go --version v1 --allow-remote
```

同样需要通过本地环境提供 `VALIDATION_TOKEN`。凭证经 stdin 传给 curl，不进入命令参数；忽略个人 `.curlrc`、保持证书验证、不跟随重定向。仅对样例的幂等 GET/PUT/DELETE 传输失败最多重试两次，HTTP 错误不重试。检查项目和样例接口仍由 `validation/check.py` 定义。不会修改本机代理配置。

## 在另一台机器安装固定工具

```sh
npm ci --prefix validation/cloud-tools --no-audit --no-fund
```

Railway 的官方安装脚本会下载对应系统架构的 CLI 二进制。Neon 需要 Node.js 20.19+。来源与接口边界见 [预检研究](../../docs/research/2026-09-16-cloud-auth-preflight.md)。
