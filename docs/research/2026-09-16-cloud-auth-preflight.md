# Railway / Neon 个人云账号验证前置检查

核实日期：2026-09-16。

本次仅查询官方公开文档与源码；没有调用用户账号 API、读取凭证、执行登录、安装 CLI 或创建云资源。命令是供后续使用的参考，实际使用前应固定版本并检查该版本的 `--help`。

## 结论

- Railway 的 npm 包仍是 `@railway/cli`，命令为 `railway`，支持浏览器登录和 `--browserless` 配对。项目内安装二进制不会隔离登录状态：当前官方源码固定读取用户目录下 `.railway/config.json`，未发现独立配置目录参数。
- Neon 当前官方首选包已是 **`neon`**，命令为 `neon login`；文档保留 `neonctl` 名称与 `auth` 的兼容说明。旧仓库已标记迁至 monorepo，不能仅依据旧 README 配置新版本。
- Neon 有 `--config-dir` 与 profile，可隔离本次认证目录；但 `NEON_API_KEY` 等环境认证优先于该目录，目录隔离不等于自动排除现有环境凭证。
- `whoami` / `me` 证明身份，不能独自证明所选 workspace/org 的免费资格。必须在用户选择目标账号与 workspace/org 后读取对应套餐与限制。

## Railway

### 安装、帮助与登录

官方安装文档给出 `npm i -g @railway/cli`，Node.js 要求为 16+；npm 包元数据公开 `railway` 可执行入口。因此可采用项目私有 npm 安装，或用 npx 指定包运行同一入口。以下是 npx 形式示意，执行时应替换为已核实的固定版本：

```text
npx --package @railway/cli@<VERSION> railway --help
npx --package @railway/cli@<VERSION> railway login --help
npx --package @railway/cli@<VERSION> railway login
npx --package @railway/cli@<VERSION> railway login --browserless
npx --package @railway/cli@<VERSION> railway whoami --json
```

来源：[CLI 安装与认证](https://docs.railway.com/guides/cli)、[官方 npm package.json](https://github.com/railwayapp/cli/blob/master/package.json)、[login](https://docs.railway.com/cli/login)、[whoami 源码](https://github.com/railwayapp/cli/blob/master/src/commands/whoami.rs)。npx 形式依据该 npm 包入口推导，本轮未执行验证。

`login --browserless` 不代表无需用户授权：CLI 输出 URL 与 pairing code，用户在浏览器登录目标账号并完成配对。当前官方文档也描述普通 `login` 在无法打开本机浏览器时回退到 device-code 流程。新账号创建与登录是同一流程，新用户需要在网页上接受条款。[login](https://docs.railway.com/cli/login)

### 配置与认证边界

当前 `Configs::root_config_path()` 用 `dirs::home_dir()` 拼接 `.railway/config.json`（production）；没有在该实现中发现 `--config-dir` 或 `RAILWAY_CONFIG_PATH` 覆盖入口。不能声称把 CLI 安装进项目目录就拥有独立登录账户。本次不改变系统 HOME、不移动或读取已有 Railway 配置。[配置源码](https://github.com/railwayapp/cli/blob/master/src/config.rs)

官方支持 `RAILWAY_TOKEN`（项目范围）和 `RAILWAY_API_TOKEN`（账号/workspace 范围）；当前文档要求不能同时设置二者。预检必须知道使用的是哪一种认证途径，但不应打印 token。项目 token 不适合充当识别任意个人账号及所有 workspace 的凭据。[login 认证变量](https://docs.railway.com/cli/login#environment-variables)

### 只读身份、目标与资格检查

1. `railway whoami --json`：当前源码输出姓名、email，以及可访问 workspace 的 `id`、`name`；不输出套餐字段。
2. 用户指定本次个人 workspace 后，`railway usage --workspace <ID> --json` 查看当前周期使用量、账单与估算；`railway usage limit status --workspace <ID> --json` 只读查看使用限制。不要执行 `limit set/update/remove`。
3. **当前 Free/Trial/Paid 套餐与 Full/Limited Trial 资格还需核对对应 workspace 的官方控制台套餐/账单/验证页面。** 本轮未确认一个稳定 CLI 命令能同时返回这些资格，因此不能把 usage 为零、登录成功或“新账号”推断为免费可用。

来源：[whoami 源码](https://github.com/railwayapp/cli/blob/master/src/commands/whoami.rs)、[usage 文档](https://docs.railway.com/cli/usage)、[官方用量 CLI 公告](https://railway.com/changelog/2026-07-10-feature-flags)、[Trial 资格](https://docs.railway.com/pricing/free-trial)。

Full/Limited Trial 是需要单独检查的条件。官方说明验证取决于连接的 GitHub 账号等因素，Limited Trial 有出站网络和端口限制；用户可在 [Railway verification](https://railway.com/verify) 启动验证。验证结果不能由工具保证，也不能自动用升级付费来绕过。[Trial 文档](https://docs.railway.com/pricing/free-trial)

## Neon

### 当前包名与登录命令

当前官方安装方式是 `npm i -g neon@latest` 或 `npx neon <command>`，要求 Node.js 20.19.0+。`neon auth` 是 `neon login` 的旧别名；文档称 `neonctl` 为兼容名称，但当前主包 package.json 的 bin 是 `neon`。已有 `neonctl` 安装应以其实际版本帮助为准，不把旧 npm 包与新主包视为同一发布物。[安装](https://neon.com/docs/reference/cli-install)、[login](https://neon.com/docs/cli/login)、[主包元数据](https://github.com/neondatabase/neon-pkgs/blob/main/packages/cli/package.json)、[旧仓库迁移说明](https://github.com/neondatabase/neonctl)

```text
npx neon@<VERSION> --help
npx neon@<VERSION> login --help
npx neon@<VERSION> --config-dir <private-config-dir> login
npx neon@<VERSION> --config-dir <private-config-dir> me --output json
npx neon@<VERSION> --config-dir <private-config-dir> orgs list --output json
```

`--config-dir` 是官方全局参数；当前源码也定义了这个参数。文档对默认目录存在新旧差异：login 页写 `~/.config/neon`，全局参数表仍写 `~/.config/neonctl`。本次建议显式指定私有配置目录，并使用同一个目录执行登录和后续只读检查，不依赖默认目录名。[全局参数](https://neon.com/docs/cli)、[参数源码](https://github.com/neondatabase/neon-pkgs/blob/main/packages/cli/src/index.ts)

### 必须由用户完成的授权

`neon login` 启动浏览器 OAuth，用户选择/登录自己的 Neon 账号并授权 CLI；随后凭证保存到本地配置。源码会打印 Auth URL，浏览器无法自动打开时可手动复制 URL。**本轮未找到等价于 Railway `--browserless` 的独立配对命令，且没有验证跨设备 localhost callback 可用。**[login](https://neon.com/docs/cli/login)、[OAuth 源码](https://github.com/neondatabase/neon-pkgs/blob/main/packages/cli/src/auth.ts)

也可由用户提供已有 API key，经 `NEON_API_KEY` 使用。官方认证优先级为 `--api-key` → `NEON_API_KEY` → 本地凭证 → 自动网页登录；因此某些看似只读的命令在未认证时会自动启动登录。不要把真实 key 写入聊天、报告、命令参数或普通项目文件。[login](https://neon.com/docs/cli/login)、[全局参数](https://neon.com/docs/cli)

通过 Vercel-managed integration 创建而没有普通 Neon 注册身份的用户，官方要求改用 Neon API key，不能保证浏览器 login 路径可用。[login 注意事项](https://neon.com/docs/cli/login)

### 只读身份、组织与套餐

- `neon me --output json`：核对当前用户；官方示例还含用户 `plan` / billing 信息，但示例存在旧时间戳，不能将其视为当前真实 org 的资格。
- `neon orgs list --output json`：列出组织及 ID；默认表格会省略字段，组织列表本身不保证包含 plan。
- 用户选定组织后，官方 `GET /organizations/{org_id}` 返回组织信息及 plan。当前 CLI 支持 API passthrough，故可用 `neon api /organizations/<ORG_ID> --method GET` 读取；实际授权范围和输出需届时验证。不要附带 body，避免默认方法变成 POST。

来源：[me](https://neon.com/docs/cli/me)、[orgs](https://neon.com/docs/cli/orgs)、[组织 API](https://neon.com/docs/manage/orgs-api)、[api 命令](https://neon.com/docs/cli/api)。本轮没有实际调用这些 API。

## 本次尚未核实

- root 最终安装的 npm 版本、二进制行为、其完整 `--help` 是否与当日官网一致。
- 用户选定的 Railway/Neon 身份、workspace/org、角色与套餐；完全没有读取用户凭证或账号资料。
- Railway 的 Free/Trial/Paid 与 Full/Limited 资格的最终只读机器字段；本轮确认了身份和用量 CLI，未宣称它们覆盖全部资格判断。
- Neon 登录回调在当前浏览器与本机网络的实际表现，API key 或 OAuth token 的实际组织权限。
- 当前套餐是否满足部署与外接数据库网络要求；本报告仅准备认证预检，不是资源创建授权，也不是云部署验收。
