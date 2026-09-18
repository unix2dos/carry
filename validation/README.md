# 多语言部署验证样例

这些是独立编写的容器兼容性验证程序。部署管理工具位于 `cmd/carry`。四个目录分别可作为容器构建根目录，业务接口一致。

已完成的本地验收与明确未验证的部分见 [RESULTS.md](RESULTS.md)。

## 一条命令验证

需要 Python 3.10+、本地 Docker 引擎和 OpenSSL。只使用 Python 标准库驱动测试；应用及其依赖在容器内构建。本机构建是开发验收方式，产品默认由目标平台构建的决定不变。

```sh
python3 validation/check.py
```

默认构建与运行 `linux/amd64`。如只验证本机 ARM 架构：

```sh
python3 validation/check.py --platform linux/arm64
```

脚本会：

- 确认使用本地 Docker unix socket，拒绝远程 Docker 主机。
- 构建四种语言镜像，创建本次专用网络和临时 PostgreSQL；不会读取已有 `DATABASE_URL` 或使用现有数据库。
- 只把应用端口绑定到 `127.0.0.1`，数据库不发布主机端口。
- 验证鉴权、参数化 SQL、Unicode、输入大小及重复写入；在临时构建目录修改版本源码、重建并替换镜像，检查已有数据保留。
- 验证数据库中断及恢复，无需重启应用。
- 验证默认 TLS 拒绝明文服务、非受信证书和主机名不匹配；显式配置测试 CA 后连接成功。
- 停止本次所有写入应用，将数据导出并恢复到第二个临时 PostgreSQL，再验证数据、自增序列、目标写入与源库保留。
- 保存脱敏日志和 JSON 报告到 `validation/artifacts/<run-id>/`，最后删除本次创建的容器、数据库卷和网络。构建镜像与构建缓存保留，不执行全局 prune。

这些检查会下载公共基础镜像和依赖并占用本机资源。只证明选定架构、本地容器与这些示例通过，不能代替云账号资格、云构建额度、跨云网络、休眠和真实账单验证。

## 公共运行约定

| 环境变量 | 含义 |
| --- | --- |
| `PORT` | 监听端口，默认 `8080`，绑定 `0.0.0.0` |
| `DATABASE_URL` | PostgreSQL URI，指向专用验证数据库 |
| `VALIDATION_TOKEN` | 至少 32 字节的随机令牌；必须提供，用于记录接口 |
| `APP_VERSION` | 健康接口显示的版本，默认 `v1` |
| `DATABASE_SSLMODE` | 默认 `verify-full`；仅本地开发时显式设 `disable` |
| `DATABASE_CA_CERT` | 可选 CA 文件路径，用于私有 CA；不支持自动忽略证书错误 |

`DATABASE_SSLMODE` 是样例的统一 TLS 策略。它覆盖 URI 中的 `ssl`、`sslmode`；URI 内的证书参数由统一策略替代。默认完整验证证书链与主机名，不允许通过 URI 的 `prefer` 或 `require` 降低校验；没有配置样例变量时，URI 中的 `sslmode=disable` 或 `ssl=0` 也不会禁用 TLS。自定义 CA 请使用 `DATABASE_CA_CERT`，不支持本样例范围外的客户端证书认证。

应用启动时在该专用数据库内创建 `validation_records` 表。不要把生产数据库连接串交给验证程序。凭证、连接串和请求值不会写入应用访问日志，数据库错误只返回固定错误码。

所有 Dockerfile 最终阶段使用非 root 用户。依赖版本记录在 Go/npm/Cargo 锁文件和 Python requirements 中，应用基础镜像固定到摘要。系统软件包仓库仍可能更新，因此不承诺按字节完全相同的可重现构建。临时 PostgreSQL 优先使用本机已有 `postgres:16`，也可通过 `--postgres-image` 指定；报告记录其镜像 ID 和架构，以及应用 v1/v2 镜像 ID。

平台的周期性存活检查可使用 `/healthz`。`/readyz` 会连接数据库，适合发布验收和按需诊断；持续调用可能妨碍免费服务休眠，不能把高频轮询默认当作免费监控。

## HTTP 接口

| 请求 | 结果 |
| --- | --- |
| `GET /healthz` | 进程存活、语言与版本；不依赖数据库，不需要令牌 |
| `GET /readyz` | 数据库可用返回 200，不可用返回 503；不需要令牌 |
| `PUT /records/<key>` | 原始 UTF-8 文本作为值；同 key 重试执行 upsert |
| `GET /records/<key>` | 读取 JSON `{key,value}`，不存在返回 404 |
| `DELETE /records/<key>` | 只删除这个 key 的记录，用于清理验证数据 |

记录接口必须带 `Authorization: Bearer <VALIDATION_TOKEN>`。key 仅限 1–80 个 ASCII 字母、数字、下划线和连字符；value 为 1–4096 字节有效 UTF-8，不包含 NUL。PUT 使用已知 Content-Length。样例采用每请求连接数据库的简化方式，适用于低频验证，不用于生产吞吐量评估。

## 后续云端 HTTP 验收

账号授权、免费计划核对、创建资源和平台构建不在本脚本中自动执行。完成单个样例的已授权部署后，可对已存在的 HTTPS 地址检查接口：

```sh
# 通过本地安全方式设置 VALIDATION_TOKEN，不要把实际令牌提交到仓库。
python3 validation/check.py --url https://your-fixture.example --language rust --allow-remote
```

此模式会创建随机测试记录，并在检查后删除该记录；不会创建云资源，不跟随 HTTP 重定向，不关闭 HTTPS 证书校验。

真实云验证应分别记录：个人测试账号、正式 Free 计划（不能以 Trial 代替）、构建成功、完整访问地址、数据库连通性、更新与恢复结果、费用模式及实际用量。当前不能据本地检查宣称已支持 Railway/Neon 的所有账号条件。
