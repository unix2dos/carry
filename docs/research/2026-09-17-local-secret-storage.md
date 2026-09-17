# 本地 Secret 存储：跨平台库与实际边界

核实日期：2026-09-17。只读调查官方文档与固定版本源码；未读取用户凭证、运行凭证 API、修改产品实现或操作云资源。

后续用户决策：优先采用受限权限的明文本地文件，移除首版对钥匙串的依赖。以下保留调查时的建议与对比，不代表当前选定实现；当前方案见 [Alpha 使用说明](../ALPHA.md)。

## 结论与最小建议

**建议保留系统凭证库方向，把 macOS Keychain 当作一个平台实现。Windows 支持需要补实现和验收，不能由“换一个跨平台库”直接承诺。** 当前 Ship 已有三个存储操作，可沿用这个小边界；暂不引入多后端自动选择、密码保险箱或同步框架。

关键取舍是 Secret 大小：Ship 当前实现的 `maxSecretSize` 为 **16 KiB**，Windows Credential Manager 的单项 `CredentialBlob` 上限为 **2,560 字节**。两个被调查的库都将整个值放进一个 blob；它们不能直接保留现有大小行为。16 KiB 是当前实现选择，并非已确认的用户需求。macOS 保留现有 Security.framework 实现即可；若确定支持 Windows，先决定只支持较小 Secret 是否足够，再评估较大值所需的系统保护方式，例如另行研究用户作用域 DPAPI。不要为迁就库静默缩小已有上限，也不要自动回退到文件。[Ship 当前限制](../../cmd/ship/secrets.go)、[Microsoft CREDENTIALW](https://learn.microsoft.com/en-us/windows/win32/api/wincred/ns-wincred-credentialw)

这些是**基于当前产品的建议**，不是外部标准要求必须采用某个 Go 包。跨平台运行、每台机器安全保存、跨机器迁移是三个独立问题；以下库主要处理前两者，不提供 Ship 的迁移协议。

## 固定版本比较

以下是调查时 GitHub 最新 release/tag，不用浮动分支作为实现证据：

| 项目 | 已核对版本 | 实际后端与构建要求 | 判断 |
| --- | --- | --- | --- |
| `zalando/go-keyring` | [v0.2.8](https://github.com/zalando/go-keyring/releases/tag/v0.2.8)，2026-03-23；commit `a8cdfe320cc8bc0534c895a648dc9214715a9da5` | macOS 调 `/usr/bin/security`；Windows 调 `wincred`；Linux/BSD 用 Secret Service / D-Bus。macOS、Windows、Linux 不要求 cgo；FreeBSD/DragonFly 构建标签另有 cgo 条件。 | API 较小、无文件回退，但 macOS 命令长度和 Windows blob 上限均不兼容现有 16 KiB 行为。 |
| `99designs/keyring` | [v1.2.2](https://github.com/99designs/keyring/releases/tag/v1.2.2)，2022-12-19；commit `929f178361c0be06c50f0a82598cbfa1a965f40f` | macOS 使用 `99designs/go-keychain`，**Keychain 后端要求 cgo**；Windows 用 `wincred`；Linux 有 Secret Service、KWallet、内核 keyring；另有非 Windows 的 `pass` 和通用加密文件后端。Windows/Linux 可无 cgo 编译。 | 提供较多策略选择，也把选择、回退、口令管理责任交给调用方；当前 Ship 不需要这层框架。 |

后端证据：[zalando macOS](https://github.com/zalando/go-keyring/blob/a8cdfe320cc8bc0534c895a648dc9214715a9da5/keyring_darwin.go)、[Windows](https://github.com/zalando/go-keyring/blob/a8cdfe320cc8bc0534c895a648dc9214715a9da5/keyring_windows.go)、[Unix 构建条件](https://github.com/zalando/go-keyring/blob/a8cdfe320cc8bc0534c895a648dc9214715a9da5/keyring_unix.go)、[99designs 后端列表/选择](https://github.com/99designs/keyring/blob/929f178361c0be06c50f0a82598cbfa1a965f40f/keyring.go)、[macOS 构建条件](https://github.com/99designs/keyring/blob/929f178361c0be06c50f0a82598cbfa1a965f40f/keychain.go)。

## 不能从包名推断的行为

### 1. zalando 的 macOS secret 经 stdin，不在进程 argv

v0.2.8 启动的是 `security -i`，再将含密码的交互命令写入标准输入。密码先做 Base64 编码；**编码不是加密**。整个命令超过 4,096 字节便返回错误，实际可保存原值小于约 3 KiB，随 service/account 长度变化。读取值经子进程输出回到 Go 内存。不要把 README 中演示的 `security ... -w password` 当成库实际采用的传参方式。[固定源码 L69–100](https://github.com/zalando/go-keyring/blob/a8cdfe320cc8bc0534c895a648dc9214715a9da5/keyring_darwin.go#L69-L100)

99designs 的 macOS/Windows/Secret Service 后端使用系统 API 或 D-Bus，不启动用于传递 Secret 的 shell；其 `pass insert` 后端也把数据放入 stdin，命令参数包含条目路径。[macOS](https://github.com/99designs/keyring/blob/929f178361c0be06c50f0a82598cbfa1a965f40f/keychain.go)、[Windows](https://github.com/99designs/keyring/blob/929f178361c0be06c50f0a82598cbfa1a965f40f/wincred.go)、[Secret Service](https://github.com/99designs/keyring/blob/929f178361c0be06c50f0a82598cbfa1a965f40f/secretservice.go)、[pass](https://github.com/99designs/keyring/blob/929f178361c0be06c50f0a82598cbfa1a965f40f/pass.go)

### 2. Windows 的限制来自系统，持久化范围也要读准

zalando 主动拒绝超过 2,560 字节的值；99designs 直接调用写入 API，未做同样的预检查，也没有分片。两者分别依赖 `wincred` v1.2.3 / v1.1.2，`NewGenericCredential` 默认 `PersistLocalMachine`：**同一用户、同一机器跨登录持久**，并非所有本机用户共享；这也不等于跨设备同步。[zalando 检查](https://github.com/zalando/go-keyring/blob/a8cdfe320cc8bc0534c895a648dc9214715a9da5/keyring_windows.go#L25-L47)、[99designs 写入](https://github.com/99designs/keyring/blob/929f178361c0be06c50f0a82598cbfa1a965f40f/wincred.go)、[wincred v1.2.3](https://github.com/danieljoos/wincred/blob/v1.2.3/wincred.go)、[v1.1.2](https://github.com/danieljoos/wincred/blob/v1.1.2/wincred.go)、[Microsoft 持久化语义](https://learn.microsoft.com/en-us/windows/win32/api/wincred/ns-wincred-credentialw)

### 3. 文件回退不是共同默认，也不是完整的备份方案

- **zalando 无文件存储后端。** 不支持的平台返回 `ErrUnsupportedPlatform`；Linux 的桌面 Secret Service、用户 D-Bus 会话及 collection 仍须可用，不能把 Linux 支持理解成任意无桌面服务器都能用。[fallback](https://github.com/zalando/go-keyring/blob/a8cdfe320cc8bc0534c895a648dc9214715a9da5/keyring_fallback.go)、[Unix 实现](https://github.com/zalando/go-keyring/blob/a8cdfe320cc8bc0534c895a648dc9214715a9da5/keyring_unix.go)
- **99designs 默认会在 Open 时依次尝试后端**，`AllowedBackends == nil` 表示允许全部可用后端；文件后端始终注册且 opener 不检查目录/口令配置。它不会在每次 Get/Set 失败后再自动切换。要坚持系统凭证库存储，应显式限定后端，不把后端选择交给运行环境碰运气。[Open](https://github.com/99designs/keyring/blob/929f178361c0be06c50f0a82598cbfa1a965f40f/keyring.go)、[Config](https://github.com/99designs/keyring/blob/929f178361c0be06c50f0a82598cbfa1a965f40f/config.go)
- 文件后端采用口令派生的 JWE（`PBES2-HS256+A128KW` / `A256GCM`），创建目录/文件时传入 `0700` / `0600`；需要调用方给出目录和口令函数。它会缓存口令于进程内存，直接 `os.WriteFile`，不提供应用级迁移、口令恢复或原子替换协议；这些权限位也不能直接代表 Windows ACL 已经过产品验收。[file.go](https://github.com/99designs/keyring/blob/929f178361c0be06c50f0a82598cbfa1a965f40f/file.go)

## 对 Ship 的实施边界（建议，未实施）

1. 保留现有 macOS 实现及“不可用就报错、不明文回退”的行为；将持久化引用和对用户文案逐步表达为本地安全存储，避免产品概念固定为 Keychain。无需先抽象出多后端注册框架。
2. 本地缓存是否存在与普通源码部署应分别建模；缓存缺失不应天然等于必须重写云端 Secret。任何改变仍应保留已提交但结果未知的写入记录与对账约束。这是产品建议，不由上述库保证。
3. Windows 先确认大小要求和登录会话场景；Linux 先确认桌面 Secret Service 是否属于支持范围。各平台要实际验证锁定/拒绝访问、重启后读取、超长值、删除及错误传播。单纯交叉编译不算存储验收；Ship 当前还使用 `syscall.Flock`，安装脚本仅接收 macOS，因此库支持 Windows 不等于 Ship 已支持 Windows。[进程锁](../../cmd/ship/store.go)、[安装脚本](../../scripts/install.sh)
4. 系统凭证库存储主要降低静态文件意外泄露风险。由于读取 API 最终把明文交给当前进程，不能承诺抵御已经完全控制 Ship 进程或当前用户运行环境的攻击者；这属于由 API 行为得出的威胁模型边界。版本历史保留、跨机器恢复和迁移还须由产品明确规定。

## 产品层补充建议（未实施）

- 普通配置、云账号登录和业务 Secret 分开：项目 ID/状态留在本地 JSON；已有官方 CLI 的登录令牌由官方工具管理；只为 Ship 确实需要复用、且用户授权保存的业务 Secret 保留本地副本。
- 默认用当前系统的凭证库，是已有 CLI 的常见做法，并非必须指定某个库。GitHub CLI 默认使用系统凭证库，同时支持其他输入方式且可能回退明文文件。对 Ship 的建议是把存储方式明确告诉用户，不在系统凭证库失败时静默改变保护方式；无桌面环境的加密文件或临时输入，按真实用户需求再加。[GitHub CLI 登录说明](https://cli.github.com/manual/gh_auth_login)
- 接入已有应用时，缺少本地缓存不应迫使用户重新设置全部云端 Secret。普通源码部署可设计为沿用现有配置，数据库目标核验另报证据范围；需要修改 Secret 才进入明确的配置写入流程。当前已有的 `unknown` 写入仍需核对或经过明确处置，不能借这项设计调整清空记录绕过。
- 加密文件仍需解决解密钥匙放哪里、如何解锁与恢复。OWASP 建议不要把未经保护的加密钥匙与其保护的秘密存放在一起。对 Ship 的当前阶段，暂不自建完整密码保险箱。[OWASP 密钥存放](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html#73-where-to-store-the-encryption-keys)
- 日志首先应避免记录密码、令牌和数据库连接串；用本地已知值匹配只是补充措施。历史密钥应有清理入口和保留范围，不能为日志匹配无限累积可用旧值。删除本地副本与撤销云端凭证是不同操作。[OWASP 日志](https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html#data-to-exclude)、[Secret 生命周期](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html#27-secret-lifecycle)

## 许可证与验证范围

两库根目录均为 MIT 许可证；zalando 的 `keyring_darwin.go` 另外保留 Google 的 Apache-2.0 文件头，分发时应保留相应第三方声明，不宜只依据仓库徽章概括全部源文件。[zalando LICENSE](https://github.com/zalando/go-keyring/blob/a8cdfe320cc8bc0534c895a648dc9214715a9da5/LICENSE)、[Darwin 文件头](https://github.com/zalando/go-keyring/blob/a8cdfe320cc8bc0534c895a648dc9214715a9da5/keyring_darwin.go#L1-L13)、[99designs LICENSE](https://github.com/99designs/keyring/blob/929f178361c0be06c50f0a82598cbfa1a965f40f/LICENSE)

使用 research / agent-reach 的 GitHub CLI 路径克隆上述 tag 到临时目录，并以 Microsoft 官方文档核对系统上限。对库包执行 `CGO_ENABLED=0 GOOS=<目标> go build .`：zalando 的 darwin/windows/linux、99designs 的 windows/linux 均编译通过；只编译库，没有运行其初始化或凭证操作。未调查替代库、未做安全审计，也未调用真实凭证库；这些结果不是 Ship 或存储的跨平台运行验收。
