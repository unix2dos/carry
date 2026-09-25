# 首条真实部署路径使用用户已有 VPS

2026-09-25 已接受。Carry 首先为作者自己解决源码上线、更新与恢复的问题，以已有 DMIT VPS 和 Loop 作为首个真实验收对象。默认复用 OCI 镜像或 Dockerfile，并以单机 Docker Compose 运行；本地管理服务和编码 Agent 仍在用户电脑上。免费云平台额度依赖供应商资格和变化中的限制，不能作为这个产品的长期运行承诺；已有 Vercel / Railway Alpha 继续受支持，但不再决定下一版首用路径。

这一决定修订了 [ADR-0003](0003-no-automatic-charges-by-default.md) 对「默认免费计算平台」的假设，以及 [ADR-0005](0005-language-neutral-container-delivery.md) 对「默认由目标平台构建」的假设；仍坚持不在未经用户明确选择时增加自动计费。VPS 上的原有服务须在发布前后核对，应用发布授权不等于整机管理授权。
