# Ship：内部试验版

本版把已验证的 Railway + Neon 链路接入 CLI 和极简网页。它关联已有资源，读取真实状态与日志，将当前 Dockerfile 项目发布到指定服务，并记录操作以供中断后核对。原型的模拟页面仍保留在 `docs/prototypes`，这里运行的是实际工具。

当前先在 macOS / arm64 验收。Go 程序仅使用标准库，浏览器资源打包在可执行文件中；沿用项目内固定版本的 Railway 5.57.2 和 Neon 4.18.0 官方 CLI。Neon CLI 仍需要 Node.js 20.19+。公开安装包、其他系统的完整验收及自动创建资源尚未完成。

## 从源码安装（macOS arm64）

准备 Go 1.24+、Node.js 20.19+ 和 npm，在源码根目录运行：

```sh
sh scripts/install.sh
cd "$HOME/.local/share/ship"
./bin/ship serve --open
```

安装程序编译 Ship，并安装锁定版本的官方 CLI 到同一目录。它不需要 sudo，不修改 shell 或全局 npm 包，也不会登录云账号或创建云资源。安装需要网络下载依赖；完成后不依赖原源码目录。其他平台尚未验收，安装程序会明确停止。

默认安装目录为 `~/.local/share/ship`；可把一个尚不存在的绝对路径作为脚本参数。已有目录不会被覆盖，升级时先安装到另一个目录。云登录与 Ship 的项目状态保存在安装目录以外，切换可执行文件不需要重新登记。

从 UpOK 升级时，先关闭旧的本地服务，再启动 Ship。若用户配置目录中还没有 `ship`，工具会沿用已有的 `upok` 目录，保留项目、授权和发布历史；`--state-dir` 可显式指定目录。旧的 `UPOK_RAILWAY_BIN` / `UPOK_NEON_BIN` 环境变量仍可用，新的 `SHIP_*` 变量优先；显式参数和已保存的工具路径维持原有优先级。旧安装可留存供已有工具路径使用。历史 `upok:` / `pdeploy:` 发布标记保持原值，新操作使用 `ship:`。

以下 `./bin/ship` 和 `./tools/…` 命令均从安装目录运行。已有部署管理记录但没有供应商登录时，仍可打开网页查看本地记录；刷新和发布需要先完成登录。

### 开发者直接构建

在项目根目录：

```sh
# 首次从源码准备；已有 bin/ship 时可直接启动。
npm ci --prefix validation/cloud-tools --no-audit --no-fund
go build -buildvcs=false -o bin/ship ./cmd/ship

./bin/ship serve --open
```

`-buildvcs=false` 让 Git 工作目录与源码归档采用相同的构建命令。构建后运行程序不需要本地 Docker 或 Go 编译器；官方 CLI 仍是运行依赖。

服务仅监听 `127.0.0.1`。启动地址的片段中含本次本地会话密钥，浏览器加载后移入会话存储并从地址栏移除；不要把完整启动地址公开。退出进程后会话失效，重新启动时使用新地址。

## 登录与关联已有项目

先通过供应商官方 CLI 完成用户自己的 OAuth 登录。沿用现有登录无需重复操作；首次登录从安装目录运行：

```sh
env -u RAILWAY_TOKEN -u RAILWAY_API_TOKEN ./tools/node_modules/.bin/railway login
env -u NEON_API_KEY -u NEON_PROFILE ./tools/node_modules/.bin/neon login \
  --config-dir "$HOME/.config/neon" --no-analytics
```

浏览器中选择自己的账号，密钥留在官方 CLI 中。Railway 使用 `~/.railway/config.json`，Neon 使用上述显式目录。已有 Neon 登录位于其他目录时，登记项目使用 `--neon-config` 指向它。源码直接构建时，官方 CLI 路径为 `validation/cloud-tools/node_modules/.bin/`。

当前需先有 Railway 服务、Neon PostgreSQL 和可用应用地址。可让现有 Agent 从用户选定的账号查询资源 ID，再调用下方登记命令；界面尚未提供自动创建资源或账号选择器。内部版不会启用付费。

新安装默认将资源标识、源码路径和工具路径保存到用户配置目录下的 `ship`（macOS 为 `~/Library/Application Support/ship`）；已有 UpOK 用户沿用上文所述旧目录。文件权限为 0600，目录为 0700；云令牌与数据库连接串不写入这些记录。

```sh
./bin/ship register \
  --name demo --source /path/to/your/project --url https://your-app.example \
  --workspace WORKSPACE_ID --railway-project PROJECT_ID \
  --service SERVICE_ID --environment ENVIRONMENT_ID \
  --neon-org ORG_ID --neon-project NEON_PROJECT_ID --neon-endpoint ENDPOINT_ID \
  --allow-publish --allow-trial
```

这些 ID 必须来自用户选择的实际资源，命令中的大写值只是占位符。登记会核对服务及环境归属、Neon 组织及端点，以及应用 `DATABASE_URL` 与端点的对应关系。省略 `--allow-publish` 创建只读关联；`--allow-trial` 表示明确接受当前试用条件。权限可通过 `authorize NAME --allow-publish=false` 等明确参数变更，正在运行的操作不会被这条命令隐式中止。

CLI 二进制位置可用 `--railway-bin`、`--neon-bin` 指定；首次登记成功后保存为本机设置。一个状态目录使用一组官方 CLI 登录上下文，需要隔离时使用另一个 `--state-dir`。

## 让现有 Agent 使用

把本次安装中的 `skills/ship/SKILL.md` 提供给能够在本机执行命令的编码 Agent，例如：“阅读这里的 Ship Skill，帮我关联自己的已有应用，并检查状态。”它与 CLI、网页使用同一份记录。此安装不会自动修改 Agent 配置或全局安装 Skill。

## 常用操作

```sh
./bin/ship list
./bin/ship status demo
./bin/ship check demo
./bin/ship logs demo
./bin/ship publish demo --detach
./bin/ship reconcile demo --wait
./bin/ship history demo
```

`status` 查询供应商管理接口，不主动访问应用；`check` 会访问 `/healthz` 与 `/readyz`，可能唤醒休眠实例。内部版采用已验证的这两个公开检查路径，不对业务数据发起写操作。发布后的服务状态与 HTTP 检查分别记录，网络超时不会被改写成供应商部署失败。

网页默认显示已有项目、访问地址、真实状态和三个常用入口，资源详情与日志按需展开。页面只轮询本地操作记录，云端查询由显式刷新或有界的发布观察触发。

## 本地业务密钥

macOS 的 cgo 构建可以把业务密钥保存到系统钥匙串。普通项目记录只保存随机引用和时间；密钥值不进入项目 JSON、命令参数或 CLI 输出，也不会上传到作者服务。官方 CLI 的云账号登录仍由官方工具管理。

```sh
# 输入文件按原始字节读取，包含结尾换行；请由用户妥善保管。
./bin/ship secret save demo DATABASE_URL --stdin < /path/to/private-database-url
./bin/ship secret list demo
./bin/ship secret check demo
```

`save` 只保存到本机，不写入云端。重复保存会增加一个版本，旧版本留在钥匙串中，用于遮蔽历史日志。`check` 只核对本地密钥可读取，始终不把本地缓存当作云端当前配置的证明；云端同步和变更核对需由对应平台的接入流程实现。

已保存的值参与已有源码上传检查和日志脱敏。钥匙串不可访问时停止这些操作，不退回明文文件。通用日志规则与已知值匹配不保证发现所有拆分、编码或应用自定义格式中的秘密。钥匙串功能需要 Apple Command Line Tools 和 `CGO_ENABLED=1`；其他平台或禁用 cgo 的构建明确返回不支持。

本机可用合成值验证钥匙串跨进程存取，测试结束会清理自己的测试项：

```sh
SHIP_TEST_KEYCHAIN=1 go test ./cmd/ship -run TestSystemKeychain -v
```

## 发布与中断

发布前重新读取账号计划、资源归属和数据库连接目标，截取源码副本，保存操作意图，再调用一次供应商上传。每次操作使用唯一的部署消息标记。若提交结果未知，新发布会被阻止；`reconcile` 在最近 100 次部署中寻找同一标记，不重新提交。

关闭网页不取消服务进程中的发布；退出进程后，云端已提交任务仍由供应商处理，重新启动后通过记录核对。持续观察最多 5 分钟，到时保留待核实状态。没有找到匹配部署不等于证明未提交，不能自动清空记录重试。

已完成的操作保留当时的结论，`reconcile` 不会因资源后来被替换而改写历史；查询当前情况使用 `status` 和 `check`。

源码副本限制为 20 MiB / 2000 个常规文件，排除常见凭证文件、依赖缓存和本地数据库文件，拒绝符号链接及已知云凭证的明文内容。记录的摘要对应捕获的源码副本；供应商还会应用上传忽略规则，它不是云端镜像摘要。此检查不能发现所有种类的硬编码秘密，发布前仍须检查项目源码。

## 费用与范围

当前唯一已实测的写入路径是明确接受 Trial 的 Railway 账号加 Neon Free；发布还要求无付费订阅或默认支付方式、Trial 额度大于零。其他计费状态保留只读能力，停止发布。正式 Free 不能用 Trial 结果代替：[费用边界](research/2026-09-16-free-plan-boundaries.md)。

本版不承担首次创建项目/数据库、修改云配置、自动付费、资源删除、数据库迁移或通用资源接管。它也没有实现无人值守运维。`PORT` 的早期差异仍未解决，常规发布结果按实际部署和检查验收。

## 开发检查

```sh
go test -race ./cmd/ship
go vet ./cmd/ship
python3 validation/install-smoke.py "$HOME/.local/share/ship"
```

安装检查从临时空白状态启动网页，核对固定 CLI 版本、页面、本地认证和来源限制，不调用云平台，也不读取现有项目记录。

测试覆盖丢失提交响应后的核对、重复提交阻止、费用与归属约束、私有状态、凭证过滤、源码上传边界和本地 HTTP 认证。配套 [Skill](../skills/ship/SKILL.md)随仓库提供，尚未全局安装，也未宣称跨 Agent 验收完成。
