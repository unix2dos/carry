# Carry：内部试验版

本版把已验证的 Railway Trial / Vercel Hobby + Neon 链路接入 CLI 和极简网页。它关联已有资源，读取真实状态与日志，将当前 Dockerfile 项目发布到指定服务，并记录操作以供中断后核对。原型的模拟页面仍保留在 `docs/prototypes`，这里运行的是实际工具。

当前先在 macOS / arm64 验收。Go 程序仅使用标准库，浏览器资源打包在可执行文件中；沿用项目内固定版本的 Railway 5.57.2、Neon 4.18.0 和 Vercel 59.20.0 官方 CLI。Neon / Vercel CLI 仍需要 Node.js；当前 Vercel 实验使用 Node.js 24。已提供 macOS arm64 预编译安装包；其他系统的完整验收及自动创建资源尚未完成。

## 安装 CLI 与 Skill（macOS arm64）

准备 Node.js 24+ 和 npm，按 [安装指南](FIRST-TRY.md) 下载预编译包。安装器自动配置 zsh / bash 的 PATH，提供 `carry` 命令，并通过符号链接把同一份 Skill 注册到 `~/.agents/skills/carry`（Codex）和 `~/.claude/skills/carry`（Claude Code）。不需要 Go、Git 或 sudo。

默认安装目录是 `~/.local/share/carry`。同版本可重复运行以补齐 PATH 与 Skill；其他版本或旧源码安装目录需先保留为备份，避免覆盖。自定义目录可把一个不存在的绝对路径传给安装脚本；命令链接与 Skill 链接仍指向这个目录。

安装器只向当前 shell 的启动文件追加一行 PATH 配置，不改动原有内容。zsh 使用 `.zshrc` / `.zprofile` 并尊重 `ZDOTDIR`；bash 使用 `.bashrc` 和已存在的登录配置，没有登录配置时使用 `.profile`。子进程不能改变已打开终端的环境，因此当前终端需执行一次 `export PATH="$HOME/.local/bin:$PATH"`，新终端会自动生效。Agent 未发现 Skill 时重启它。

新安装默认使用 `~/.carry`；该目录不存在时依次沿用 `~/.ship`、用户配置目录下的 `carry` / `ship` / `upok`，保留项目、授权、密钥及发布历史，不自动复制或改写这些记录。`--state-dir` 可显式指定目录。环境变量按 `CARRY_*`、`SHIP_*`、`UPOK_*` 的顺序读取；显式参数与已保存工具路径仍优先。旧的 `ship:` / `upok:` / `pdeploy:` 操作和 `ship_operation` 平台标记继续按原值核对，新操作使用 `carry:` 与 `carry_operation`。

从默认 Ship 安装升级时，安装器保留旧程序目录，将它创建的 `ship` 命令链接转为 Carry 的兼容别名，并用 Carry Skill 替代旧的 Ship Skill 注册链接。自定义的同名文件或目录不会删除。已有 PATH 配置可继续使用。

如需卸载，只移除本安装创建的 `~/.local/bin/carry`、上述两个 Skill 链接、启动文件中带 `# carry` 的一行及安装目录；如安装器更新过 `ship` 兼容链接，也可移除该链接。`~/.carry` 或沿用的 `~/.ship` 保存用户数据，独立保留。

### 开发者直接构建

在项目根目录：

```sh
# 首次从源码准备；已有 bin/carry 时可直接启动。
(cd validation/cloud-tools && npm ci --no-audit --no-fund)
go build -buildvcs=false -o bin/carry ./cmd/carry

./bin/carry serve --open
```

`-buildvcs=false` 让 Git 工作目录与源码归档采用相同的构建命令。构建后运行程序不需要本地 Docker 或 Go 编译器；官方 CLI 仍是运行依赖。

服务仅监听 `127.0.0.1`。启动地址的片段中含本次本地会话密钥，浏览器加载后移入会话存储并从地址栏移除；不要把完整启动地址公开。退出进程后会话失效，重新启动时使用新地址。

## 登录与关联已有项目

新项目默认使用 Vercel；Neon 按需关联，后续发布沿用项目已保存的平台绑定。先通过供应商官方 CLI 完成用户自己的登录授权。沿用现有登录无需重复操作；默认安装的首次登录命令如下；自定义安装时替换供应商工具的目录：

```sh
mkdir -p "$HOME/.config/carry-vercel"
chmod 700 "$HOME/.config/carry-vercel"
env -u VERCEL_TOKEN -u VERCEL_ORG_ID -u VERCEL_PROJECT_ID \
  "$HOME/.local/share/carry/tools/node_modules/.bin/vercel" login --global-config "$HOME/.config/carry-vercel"
```

浏览器中选择自己的账号，密钥留在官方 CLI 的上述目录中。已有登录位于其他目录时，登记项目使用 `--vercel-config`、`--neon-config` 指向它。源码直接构建时，官方 CLI 路径为 `validation/cloud-tools/node_modules/.bin/`。

当前需先有 Railway 服务或 Vercel 项目及可用应用地址。数据库可选：不关联 Neon 时无需 Neon 登录、数据库或 `DATABASE_URL`，Carry 只管理应用。可让现有 Agent 从用户选定的账号查询资源 ID，再调用下方登记命令；界面尚未提供自动创建资源或账号选择器。内部版不会启用付费。

新安装默认将资源标识、源码路径和工具路径保存到 `~/.carry`；已有用户按上文规则沿用旧目录或显式迁移。macOS/Linux 文件权限为 0600，目录为 0700；普通项目记录不包含密钥值，业务密钥单独保存在 `secrets/项目名.json`。官方 CLI 的云账号令牌继续由官方工具管理。

```sh
carry --vercel-config "$HOME/.config/carry-vercel" register \
  --name demo --source /path/to/your/project --url https://your-app.example \
  --vercel-team TEAM_ID --vercel-project PROJECT_ID \
  --allow-publish --allow-hobby
```

这些 ID 必须来自用户选择的实际资源，命令中的大写值只是占位符。登记会核对应用资源归属；关联 Neon 时才核对数据库组织及端点。Vercel 隐藏的数据库连接地址仍明确标为未核验。源码需满足下文的 [Vercel 接入边界](#vercel-接入的当前边界)。`--allow-hobby` 表示用户接受个人非商业用途限制，不因选择默认平台而自动接受。省略 `--allow-publish` 创建只读关联；权限可通过 `authorize NAME --allow-publish=false` 等明确参数变更，正在运行的操作不会被这条命令隐式中止。

### 按需关联 Neon

如果应用需要 Neon，先通过其官方 CLI 登录：

```sh
env -u NEON_API_KEY -u NEON_PROFILE "$HOME/.local/share/carry/tools/node_modules/.bin/neon" login \
  --config-dir "$HOME/.config/neon" --no-analytics
```

在首次 `register` 命令中额外填写 `--neon-org ORG_ID --neon-project NEON_PROJECT_ID --neon-endpoint ENDPOINT_ID`。三个参数必须全部填写或全部省略，防止只核验一半的数据库关联。没有关联时状态记录为 `not_managed`，不声称应用没有使用其他数据库；供应商已有变量仍会保留并参与可用范围内的脱敏。

已有 Neon 关联继续沿用，不因升级而消失。安装包仍随附 Neon CLI，但使用无数据库项目不会调用它或要求注册 Neon 账号。

### 选择 Railway

已有 Railway 项目保持原平台，包括没有 `provider` 字段的旧记录。新关联需要显式选择 Railway，并通过其官方 CLI 登录：

```sh
env -u RAILWAY_TOKEN -u RAILWAY_API_TOKEN "$HOME/.local/share/carry/tools/node_modules/.bin/railway" login
carry register --provider railway \
  --name demo --source /path/to/your/project --url https://your-app.example \
  --workspace WORKSPACE_ID --railway-project PROJECT_ID \
  --service SERVICE_ID --environment ENVIRONMENT_ID \
  --allow-publish --allow-trial
```

Railway 登录保存在 `~/.railway/config.json`。关联 Neon 时，此路径还会核对应用 `DATABASE_URL` 与 Neon 端点的对应关系；`--allow-trial` 表示明确接受当前试用条件。

CLI 二进制位置可用 `--vercel-bin`、`--railway-bin`、`--neon-bin` 指定；首次登记成功后保存为本机设置。一个状态目录使用一组官方 CLI 登录上下文，需要隔离时使用另一个 `--state-dir`。

## 让现有 Agent 使用

安装时已注册 Carry Skill。Codex 使用 `$carry`，Claude Code 使用 `/carry`；其他能读取 Skill 的 Agent 可手动读取安装目录中的 `skills/carry/SKILL.md`。Skill 与 CLI、网页共用同一份本地记录。目录规则见 [OpenAI Docs](https://learn.chatgpt.com/docs/build-skills#where-codex-loads-local-skills) 与 [Claude Code 文档](https://code.claude.com/docs/en/skills#choose-where-skills-load)。

## 常用操作

```sh
carry list
carry status demo
carry check demo
carry logs demo
carry publish demo --detach
carry reconcile demo --wait
carry history demo
```

`status` 查询供应商管理接口，不主动访问应用；`check` 默认只访问 `/healthz`，关联 Neon 后再检查 `/readyz`，可能唤醒休眠实例。应用应通过对应的公开接口报告状态，不对业务数据发起写操作。发布后的服务状态与 HTTP 检查分别记录，网络超时不会被改写成供应商部署失败。

网页默认显示已有项目、访问地址、真实状态和三个常用入口，资源详情与日志按需展开。页面只轮询本地操作记录，云端查询由显式刷新或有界的发布观察触发。

## 本地业务密钥

业务密钥以**明文 JSON**保存在 `~/.carry/secrets/项目名.json`；使用 `--state-dir` 时位于所选目录的 `secrets/` 下。每个项目独立加锁并原子写入。macOS/Linux 上目录为 0700、文件为 0600；读取时拒绝符号链接、特殊文件和其他用户可访问的密钥文件。此方案不抵御同一用户下有文件读取权限的程序；不要将密钥目录提交、打包或同步给他人。

```sh
# 输入文件按原始 UTF-8 文本读取，包含结尾换行；请由用户妥善保管。
carry secret save demo DATABASE_URL --stdin < /path/to/private-database-url
carry secret list demo
carry secret check demo
```

`save` 只保存本地副本，不写云端。重复保存保留版本，用于遮蔽历史日志。`list` 仅返回名称、版本数与状态；`check` 只检查本地读取，不输出密钥，也不证明云端当前值。秘密通过标准输入接收，不放进命令参数或普通项目记录。整个状态目录被排除在源码上传之外；当前没有默认打包该目录的导出功能。

原实验版只保存钥匙串引用的记录，需要先迁移密钥值。迁移应保留引用 ID、时间、云端元数据与 `unknown` 状态，不以迁移触发云端写入。旧记录缺少本地值时明确报错，不能按空值继续。用户本机的本轮迁移与验证单独记录。

Vercel `secret apply NAME KEY` / `secret reconcile NAME KEY` 是独立的配置操作。普通业务 Secret 可用于未关联 Neon 的应用。通过 Carry 写入 `DATABASE_URL` 仍需已关联的 Neon 端点以核对目标；其他数据库的连接配置可继续由供应商管理。创建和更新请求契约已用临时合成变量验证；业务 Secret 没有重写，端到端普通发布不依赖这两个命令。`apply` 是明确的云端配置写入；`reconcile` 只核对上次标记。未知写入继续保留，不自动重发。存储方式变更不取消这条边界。参见 [当前接入问题](../validation/VERCEL-RESULTS.md)。

已保存的值参与源码检查和日志遮蔽；本地文件不可读取时停止依赖它的操作。通用规则和已知值匹配不能发现所有形式的硬编码秘密或日志泄露。运行 Carry 不再需要 macOS 钥匙串或 cgo。当前安装器仍只验收 macOS arm64；Windows 的访问控制和文件锁尚未适配，不能把移除钥匙串依赖视为 Windows 已可用。

## 发布与中断

发布前重新读取账号计划、资源归属和数据库连接目标，截取源码副本，保存操作意图，再调用一次供应商上传。每次操作使用唯一的部署消息标记。若提交结果未知，新发布会被阻止；`reconcile` 在最近 100 次部署中寻找同一标记，不重新提交。

关闭网页不取消服务进程中的发布；退出进程后，云端已提交任务仍由供应商处理，重新启动后通过记录核对。持续观察最多 5 分钟，到时保留待核实状态。没有找到匹配部署不等于证明未提交，不能自动清空记录重试。

已完成的操作保留当时的结论，`reconcile` 不会因资源后来被替换而改写历史；查询当前情况使用 `status` 和 `check`。

源码副本限制为 20 MiB / 2000 个常规文件，排除常见凭证文件、依赖缓存和本地数据库文件，拒绝符号链接及已知云凭证的明文内容。记录的摘要对应捕获的源码副本；供应商还会应用上传忽略规则，它不是云端镜像摘要。此检查不能发现所有种类的硬编码秘密，发布前仍须检查项目源码。

## Vercel 接入的当前边界

本版已用 Carry CLI 验收已有 Vercel Hobby 项目与 Neon Free 的发布、更新、读写与数据保留。使用已登录的官方 CLI，通过 `--vercel-config` 指定认证目录，登记时指定 `--provider vercel --vercel-team TEAM_ID --vercel-project PROJECT_ID --allow-hobby`，并提供已有应用 URL；Neon 标识仅在需要关联数据库时提供。`--allow-hobby` 表示用户接受个人非商业用途限制，不代表所有应用都适用。源码目前要求 `Dockerfile.vercel` 和仅含 `{"framework":"container"}` 的 `vercel.json`。

普通源码发布沿用平台上已有的 Secret，不强制读回、缓存或重写密钥。数据库地址不可读或本地副本已过期时，明确显示“连接目标未核验”；账号归属、套餐条件与变量存在性仍会检查。只有用户明确请求配置变更时才调用 `secret apply`。真正未知的写入仍会阻止后续发布，不能通过删除缓存绕过。

日志按可用的本地已知值和通用格式遮蔽；平台隐藏的、没有本地副本的值可能无法精确匹配，分享前需检查。`/readyz` 成功只说明应用报告数据库就绪，不独立证明连接的是登记的实例。

隔离实验已证实：Secret 更新请求包含 `key` 字段时，Vercel 即使收到相同名字也返回400；移除该字段后更新成功。创建请求仍必须包含 `key`。工具已保留回归检查，并将这类明确拒绝与网络失败等未知结果区分。历史数据库写入没有原始错误回执，用户已明确同意按“经诊断未生效”归档，原引用、标记、时间和诊断证据保留，未重发数据库连接串。[诊断结果](../validation/results/vercel-secret-update-diagnosis.json)

## 费用与范围

Railway 路径要求明确接受 Trial、无付费订阅或默认支付方式、Trial 额度大于零。Vercel 路径要求当前账号为 Hobby，且用户明确接受个人非商业用途条件。选择关联 Neon 时，才要求该组织为 Free；未关联数据库不会跳过计算平台的费用与归属核验。其他计费状态停止发布；不把 Railway Trial 结果当成正式 Free：[费用边界](research/2026-09-16-free-plan-boundaries.md)。

本版不承担首次创建项目/数据库、自动付费、资源删除、数据库迁移或通用资源接管；云配置写入仅限明确授权的 Vercel Secret 操作。它也没有实现无人值守运维。`PORT` 的早期差异仍未解决，常规发布结果按实际部署和检查验收。

## 开发检查

```sh
go test -race ./cmd/carry
go vet ./cmd/carry
python3 validation/install-smoke.py "$HOME/.local/share/carry"
```

安装检查从临时空白状态启动网页，核对固定 CLI 版本、页面、本地认证和来源限制，不调用云平台，也不读取现有项目记录。

测试覆盖丢失提交响应后的核对、重复提交阻止、费用与归属约束、私有状态、凭证过滤、源码上传边界和本地 HTTP 认证。配套 [Skill](../skills/carry/SKILL.md)随安装器注册。目录与安装行为有本地检查，独立开发者的完整跨 Agent 试用仍待完成。

## 构建发布包

维护者在源码目录运行 `sh scripts/package.sh v0.1.0-alpha.2`，生成带版本号的 macOS arm64 包、SHA-256 校验文件和安装脚本。打包清单仅包含二进制、文档、Skill、依赖清单与许可证；发布包不包含本机状态或云凭证。生成后单独发布到对应 GitHub Release。
