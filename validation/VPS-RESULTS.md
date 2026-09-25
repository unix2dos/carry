# Loop → DMIT 首次公开部署验收

日期：2026-09-25。范围：作者已有 DMIT VPS 上的一个 Loop Web 应用，公开域名 `loop.liuvv.com`。这是单机、单项目验证，不代表数据库、对象存储或其他用户服务器已受支持。

## 已完成的证据

- 源码：Loop 本地提交 `a8f89b3e71f77898965b5d277e5878631a4588a0`；Carry 本地提交 `b32ebd0`。Carry 最终发布操作 `1790344803430908000-ca9c36e4537af123`，打包源码 SHA-256 为 `879534745dccf6587ad382884ecd4c50e7b09532279b855eaa833bd7c2aca8b9`，远端容器镜像 ID 为 `sha256:9cfb0a71bde19c1232d32458cb7d28951fbaf6c9be4ae0f7dd67791d8ad4c78f`。
- 发布路径：本机按目标 `linux/amd64` 构建，镜像经 Docker SSH 传到 VPS，Compose 启动应用与 Caddy。首次发布因 Carry 暂存源码目录权限导致非 root Loop 无法读取 `web/dist`；共用打包逻辑修复后，容器回读 `READY`。再次发布后已有持久卷标记仍在，说明容器换版未清空挂载目录。
- 公开入口：Cloudflare 与 Google DNS 均返回 DMIT 的 A 记录；Caddy 日志显示证书取得成功。本机与服务器分别以正常 TLS 校验访问 `https://loop.liuvv.com/healthz` 返回 200；Carry `check loop-vps` 也返回 200。一次网页抓取工具仍报不可访问，未据此推断站点故障。
- 模型：Z.AI 当前将 `glm-4.5-flash` 标为免费；专用 Key 的直接 Chat Completions 调用返回 200 并产生 `tool_calls`。公开 Loop 上进行 **1 次**真实业务任务：4 次模型请求、3 次工具调用，`read_file` 成功，最终回答引用示例 `agent-loop.md`。`glm-4.7-flash` 在前一轮验证中超时并返回供应商 `1305` 过载，故未用于首发。[Z.AI 价格](https://docs.z.ai/guides/overview/pricing) · [错误码](https://docs.z.ai/api-reference/api-code)
- 匿名隔离：两个独立 Cookie 会话中，访客 B 无法在列表、详情或续接接口获取访客 A 的真实任务；详情和续接均返回 404。公网响应不携带访客归属哈希。完成的任务记录在挂载目录中，自动清理期限设为 7 天。
- 原有服务：Docker 安装前后及 Loop 发布后，`x-ui` 均为 active，xray 的 8443 监听保留。此检查只覆盖服务与监听，不替代 Mac 客户端的完整代理业务验收。

## 尚未证明

- 7 天自然到期和全站每日额度耗尽只通过本地自动检查，没有消耗公网额度或等待七天做实测。
- VPS 镜像回退由假 Docker 读回测试覆盖，尚未在公开实例上做真实回退；当前配置、密钥和数据不会随镜像回退。
- 未做外部开发者首次使用、持续负载、整机故障恢复或异地备份验收。DMIT 条款要求客户自行备份；当前本机卷只能证明换容器后保留数据。[DMIT 条款](https://www.dmit.io/pages/tos)
- 免费模型价格依据官方当日价格表；未取得供应商账单读回，不能把一次免费模型调用扩大为长期免费或稳定可用承诺。
