# 本地系统服务与环境下发

当前为实验实现。已完成平台适配器、服务配置生成和合成数据测试；软件机器保护与加密 Store 基础已补充，正式 POSIX 双签设备信任、持钥 boot 与真实验证同步已有合成端到端证据，物理开机和原生权限验收仍有门槛，不能宣传生产可用。测试没有导入宿主真实环境、凭据或账号，未在宿主安装服务、修改 shell 启动文件或注册表。

## 按本地用户隔离

一个设备上的每个本地用户都有独立实例、状态目录、激活排序、override 与恢复记录。daemon 参数必须包含明确 UID 或用户 SID，并在运行时校验实际身份；账号登录不能代替设备授权。

| 平台 | 服务配置 | 权限边界与尚需验证的项目 |
| --- | --- | --- |
| macOS | `LaunchDaemon`，标签包含 UID，`UserName` 为目标非 root 用户，`RunAtLoad`，`Umask=0077` | 管理员控制二进制和服务配置；目标用户只读写自己的状态目录。UID 与用户名对应关系必须校验。真实开机、无人登录、撤销和权限隔离尚未运行 |
| Linux | systemd 系统单元，目标非 root `User=`，`NoNewPrivileges`，空 capability，`ProtectSystem=strict`，`ProtectHome=true`，单独 `ReadWritePaths` | 生成器要求二进制在 `/usr/local/`，状态严格为 `/var/lib/harmonia/<UID>`，避免受保护 home 中的路径不可访问。已验证新建普通 UID 的 systemd 非交互启动、当前用户 CLI 与回环 SSH。另建隔离来宾真实入网后 init 重启、无目标用户登录的新 boot/pull 已通过；物理内核开机未跑。OrbStack LXC 全局覆盖关闭 systemd 沙盒，需完整 Linux VM 验证其生效 |
| Windows | 绑定目标用户 SID 的 `NT SERVICE\Harmonia-<SID摘要>` 虚拟服务身份；SCM 运行适配器支持停止和关机取消 | 只打开 `HKEY_USERS\<SID>\Environment`，禁止把服务身份的 HKCU 当目标账号。需要精确目录/注册表 ACL。默认已加载 hive 适配器在目标 hive 缺失时失败；另有未接入正式 daemon 的受托 token profile lease 源码，无人登录 token/broker/native 验收仍未完成 |

生成器只生成配置和清单，不安装服务、不授予 ACL。样例中的用户名、UID、SID 和目录均为虚构值。正式 POSIX daemon 已接受 `--local-directory` 的加密状态与经过原生入网验证的信任上下文，能用独立设备签名钥完成 boot challenge 后取得设备绑定 session，再执行经验证的同步；不依赖密码等价凭据。没有已完成可信 context 时只提供隔离 IPC/恢复，不联网，也不自动入网。Windows 正式 daemon 仍明确关闭，等待原生 DPAPI/SID/SCM/hive 验收。三平台真正开机验收范围见下表；既有 fixture 结果只证明合成本地行为，测试标记不能取消或作为可信注册捷径。

## 开机凭据存储的取舍

服务需要在用户登录前验证已有设备授权，不能把唯一解密钥放在只登录后才解锁的用户 Keychain 中。已在独立 `core-go/localkeys` 实现服务用户可读取的软件机器钥与 XChaCha20-Poly1305 加密 Store。目录和文件必须绑定当前非 root UID、`0700/0600`、无符号链接/异常硬链接/扩展 ACL，机器钥丢失且旧密文存在时拒绝重置。独占锁、重启与损坏负测已在 macOS/Ubuntu 合成数据验证。Windows DPAPI machine scope 与受保护 DACL 目前只有交叉编译证据，仍待原生 VM 验证。

启动前的磁盘解锁要求保留，不绕过 FileVault、LUKS 或 BitLocker。自动启动必然要求已启动系统能够使用某种机器保护材料；管理员、被攻陷的 root 或已经解锁并可读取保护材料的系统可能获得服务明文。磁盘加密主要保护未解锁的离线磁盘，不能承诺抵抗已控制系统的管理员。平台保护的软件钥匙不能统一宣称始终在硬件内。

POSIX 状态与片段使用专用 `0700` 目录和 `0600` 文件；写入通过临时文件、文件同步、原子替换与目录同步完成，拒绝非工具文件、符号链接和不私密的现有文件。Windows 的 Unix mode 位不能替代 ACL，实际 ACL 与 Windows 崩溃持久化仍需原生 VM 验证。片段包含供 shell 使用的值，属于本地敏感配置；当前 fixture 仅使用合成值。

## POSIX 原值与逐 key 下发

服务不能从外部改写已运行进程的环境。`POSIXProvider` 只保存下发目标，仅读显式提供的测试 baseline 或自身缓存，不枚举 `os.Environ`。engine 的 fixture baseline 与每个 shell 的原值是两层：每个 shell 首次 source 时只记录被接管 key 的存在性及原值，再应用下发值。

变量名只接受可移植的 shell 名称；内部 `__HARMONIA_` 命名空间保留。值以安全单引号字面量编码，包含引号、换行、`$()` 或反引号的合成值不会执行命令。空字符串与不存在分别记录。外部修改托管值时，正常刷新会纠正；无关变量和其他文件内容不变。shell 的只读内建变量不能强行改写。

最后一个来源消失时，片段输出逐 key release：恢复该 shell 记录的原值，或删除工具新增 key，并清除对应内部记录。不会把整份 shell 文件或整份环境快照恢复。其他环境仍提供同名 key 时，先回退到该来源。旧 shell 需要执行刷新才能观察更改；已经获得的明文不能追回。

暂停状态与每个 key 的本地修订号一起持久化。普通刷新每次纠正；暂停后，已见修订不再重复纠正，新的 shell 仍可一次应用保留配置。已收到撤销或到期造成来源变更时，新修订仍一次下发到剩余来源；全部来源消失时仍逐 key 恢复。暂停不屏蔽安全删除。片段状态先持久化再写片段，中途崩溃后重新打开会根据状态重建片段。

bash 使用 `PROMPT_COMMAND`，保留原来的字符串或数组内容；zsh 使用 `precmd` hook。sh 没有可移植的 prompt hook，当前只提供会话开始刷新与显式 `harmonia_refresh`；**sh 自动持续纠正尚未完成**。已在专用回环 SSH 的真实 Bash 交互会话验证 shell hook、暂停和退出恢复；VM 未装 zsh，zsh SSH 仍未跑。GUI、容器、cron 和其他业务服务的环境接入不在首版范围。

当前为保证老 shell 能执行 release，历史 release tombstone 与修订号不主动裁剪，可能随历史 key 数量增长；尚未设计可靠的清理边界。正常关闭 CLI 不清配置；停同步保留配置；崩溃后重开幂等收敛。真实卸载流程尚未实现，不能声称已完成系统清理。

## Windows 原值与通知

`WindowsProvider` 仅读取点名 key。Windows 名称不区分大小写，同批 `PATH`/`path` 等冲突会拒绝。原值记录包含不存在/存在、字面值、`REG_SZ` 或 `REG_EXPAND_SZ` 类型；不展开 `%NAME%` 引用。正式接入必须使用 `NewSecureWindowsProvider` 将原值类型写入 AEAD；`NewPersistentWindowsProvider` 的明文文件只用于 fixture 验证，原值在接管前落盘并绑定 SID，release 后清除记录，下次接管重新采集。只有内存的 `NewWindowsProvider` 用于隔离单次测试。

当前原生适配器只访问已加载的目标用户 hive，并发送 `WM_SETTINGCHANGE` 通知。Session 0 的广播不保证进入用户交互会话；新进程读取用户环境的完整生命周期仍需验收。已有进程的 env 不能被外部强制改写。默认适配器不自动加载用户 profile、不要求备份/恢复特权。新增 profile lease 的受托 token 原生适配器仍隔离在正式入口之外，权限与生命周期见下节；不写其他用户或系统环境。

## 本轮真实验证

| 验证 | 结果 | 证据与范围 |
| --- | --- | --- |
| macOS `go test ./platform -race -count=1 -v` | 通过 | sh/bash/zsh 字面量、纠正、逐 key 恢复；持久化重建；拒绝覆盖无关文件/符号链接；Windows 假存储跨重启恢复类型与二次接管；UID/SID/模板校验；bash prompt 数组保留；engine→实际 sh 暂停/撤销回退集成 |
| Windows amd64 平台测试交叉编译 | 通过 | `GOOS=windows GOARCH=amd64 go test -c ./platform`，只编译 SCM/registry 代码，未在 Windows 执行 |
| macOS `plutil -lint` | 通过 | 合成 LaunchDaemon plist 解析通过，未安装或启动 |
| OrbStack Ubuntu ARM64 平台测试 | 通过，zsh 跳过 | 临时目录、`env -i` 的 sh/bash 子进程和合成存储；VM 无 zsh；不改用户启动文件或现有环境 |
| Ubuntu `systemd-analyze verify` | 修复后通过 | 在临时 fake root 中放合成可执行占位与依赖 target，只验证配置，不执行服务。曾发现 `WorkingDirectory` 引号不被该 directive 解码，修复为绑定 UID 的绝对路径后复验通过 |
| Ubuntu 系统服务非交互启动/重启与按用户隔离 | 通过 | 新普通测试 UID、空 capability、0700 状态/0600 IPC；第二 UID 拒绝；只重启本次服务，没有重启机器 |
| Ubuntu 真实 SSH Bash 与 CLI | 通过 | 新合成密钥与回环专用 sshd；合并/优先级/override/纠正/暂停/服务重启/离线到期回退/逐 key 退出恢复；临时资源清理通过 |
| systemd 沙盒实际生效 | 未验证 | OrbStack Ubuntu 报告 LXC，全局 `zzz-lxc-service.conf` 覆盖关闭 `NoNewPrivileges/ProtectHome/ProtectSystem` 等；未修改该配置 |
| 新隔离 Ubuntu 来宾 init 重启后无登录真实授权 | 通过 | 来宾内原生 SPAKE2 上游 6/6、firstroot 双签、新设备入网、签名共享写；删除登录 slot/引导输入及旧服务端 sessions 后，重启仅新机，init/两 unit InvocationID 改变，目标 UID 无登录 session，新 boot challenge/session/pull 200 且无密码登录；正式 IPC、真实 sh 和第二 UID 拒绝通过 |
| 三平台物理内核开机、磁盘解锁与完整原生权限 | 未跑 | OrbStack 检测为 LXC，namespace boot ID 变化但共享内核 uptime 连续；不能把该 init 重启当物理 kernel boot。Windows hive 生命周期及完整安装器仍有门槛 |
| UTM Windows/macOS 原生服务 | 原生服务未跑，精确缺口见右列 | 两台既有VM仍started。Windows历史guest-agent -10004现已解除：官方exec输出精确合成标记、文件往返、SYSTEM/Session0/build26100只读确认通过；正式SYSTEM broker/S4U token/profile/IPC正在实现，未声称boot通过。macOS Apple后端不支持exec，当前无CUA且Accessibility false；官方只读网络为shared1项、serial0、queryIP0地址。没有读取真实密码/钥匙或更改登录、安全、远程登录设置 |

可复现命令：在 `core-go` 执行平台测试；在 Linux 执行 `sh service-templates/verify-linux.sh service-templates/examples/linux-harmonia-user-10001.service`。服务样例在 `core-go/service-templates/examples`。测试数据与输出没有真实凭据。新增 OrbStack 验收脚本与脱敏 JSON 位于 `core-go/service-templates`；官方 OpenSSH/acl 测试依赖保留，默认 SSH service/socket 都是 inactive，socket 为 disabled。临时测试账号、系统单元、目录、专用 sshd 与合成密钥已清理。

## M2 加密 Store 补充

`localkeys.OpenEncryptedStateStore` 实现 `localstate.Store`，由后台服务独占持有；当前用户 CLI 通过有身份校验的 IPC 请求，不获取 vault 原始字节或钥匙。固定 slot 存加密状态、独立 Ed25519/X25519 设备材料、HTTPS 登录 session、入网绑定回执及 provider 元数据，拒绝 `Synthetic` fixture，不导入/迁移现有明文 fixture。详见 `core-go/localkeys/README.md`。

Unix 默认软件机器钥是权限保护的可读随机文件，服务在系统磁盘解锁后不需要登录 Keychain。它不抵抗已控制同一用户、服务进程、root 或解锁磁盘的攻击者，也不保证硬件常驻或防止整目录历史回滚。Windows machine scope 本身不隔离同机用户，文件 DACL 是必要边界。[Microsoft DPAPI 文档](https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata)

安全 constructor 已将 POSIX 修订/暂停/release 元数据和 Windows 原值类型接入 AEAD，并在重启、真实 sh 与隔离 Windows 假存储中验证。shell 要直接 source 的 `environment.sh` 仍为权限保护的本地明文，写入使用固定 Vault 目录句柄与所有权/ACL/原子检查。旧 fixture 元数据不会自动迁移，正式路径必须明确选择安全 constructor。账号退出/切换还须编排同步取消、在途结果 epoch 拒绝与 device/session/trust 删除。恢复用 Originals 保留到逐 key 恢复成功，不因钥匙清理整文件覆盖。

`TrustContext` 不含私钥；保存/加载核对同 Vault 的设备双公钥及可选 session 的账号绑定。待完成入网回执 `Accepted=false` 可重启后查询服务器结果，再确认完成；入网 profile、manager 公钥结构与证书 JSON 大小在本地校验，证书/配对/授权验签仍由可信控制器负责，布尔完成标记本身不证明设备信任。带云数据的加密 StateStore 必须匹配同目录已完成 context 的账号 generation；发现不一致立即失败。

## 新隔离来宾的正式 boot 证据

`core-go/service-templates/boot-test` 保存本次可复核合成输入与流程，`orbstack-boot-result.json` 保存脱敏结果。只创建并重启新的 Ubuntu 24.04 ARM64 来宾，无宿主 home/SSH agent 共享。设备 UID 30001 与 server UID 30002 各有私密目录；本次普通 UID 服务 cap 为 0，0700/0600 和第二 UID 拒绝均通过。共享值来自服务端签名密文经正常 pull/HPKE/AEAD 下发，未用 fixture env 或测试 trust 布尔绕过替代入网。TLS 使用进程显式 CA 文件并保留主机名/链校验。

整个实验没有重启既有 Ubuntu、UTM 或宿主，没有修改全局 LXC 沙盒、电源、代理或系统 CA。两台既有UTM仍运行；历史执行能力缺口后来已在Windows官方API解除，但Windows正式服务门槛仍关闭，macOS真实验收仍缺合法入口。新测试来宾在清理本次账号/units/keys/SQLite/缓存后正常停机；官方工具链与公开源码保留用于后续隔离验收。服务模板已改为正式 `--local-directory`/`--local-user`，fixture 脚本保持明确隔离路径；模板解析通过不等于原生服务安装或权限完成。


## 暂停期间的缓存数据来源与当前授权

普通读取把环境的 `KeyVersion`、`GrantGeneration`、已验读授权 hash 和 canonical fingerprint 一起保存为数据来源；当前授权仍以加密来源账本的精确 target 及 `GrantCheckpoints/GrantFingerprints` 为准。暂停授权刷新只推进 `AuthorizationSequence`、当前授权检查点和账本，不推进普通 `Sequence` 或 `SeenMutations`，不解密或下发新的变量值。

收到纯 KV 轮换时，只有全图验签通过、连续的真实签名 before/after 证明同账号 generation、同设备 Ed25519/X25519 双公钥、精确旧 grant hash → 新 grant hash，才可保留旧的已验值与原数据 KV/GG。保存的路径每段都复验：同 KV 下一授权代际，或 exact origin 绑定的下一 KV/下一代际；缺签来源、跳代际、错误公钥、错误当前 target、循环及篡改指纹均拒绝。角色与期限取原缓存、全部路径和当前授权的最小边界；暂停期间不能据升级或续期扩大本地权限。路径受既有 512 条 authority 上限约束，缺完整证据不能降级成服务器公钥首次信任。

重启在使用缓存前重新验证原始固定根、完整来源图、数据来源 fingerprint、连续路径与精确当前 target；来源与值同一 AEAD 状态事务落盘。没有新来源字段的旧缓存只能在 KV/GG/fingerprint/权限均与受保护旧证据精确一致时首次识别。撤销、失去自身环境 scope、删除和到期立即删除缓存来源与 override，并逐 key 回退剩余环境或恢复原值；暂停不屏蔽这些安全变化。恢复同步仍从零普通拉取，通过当前 HPKE 封套、所有内部签名及已见检查点后，才以当前授权重建新的数据来源。

本轮 `localstate`、`syncclient` race 回归通过，覆盖两次连续轮换与漏收补链、11 类来源/账本篡改、保存失败的原子性、旧缓存严格首次识别及同序号调用方不变。真实 HTTPS/SQLite、空 vault 注册与邮件证明、首次双签初始化、固定 BoringSSL 双向 PAKE、加密 Store 重启的联合回归已通过：暂停纯轮换保留配置，以及后续签名撤销、非最后环境删除、离线截止与服务端真实到期、降权/期限缩短和升级/续期延后到完整恢复。该结果不增加三平台物理无人登录开机或完整安全审计的证据。


## Windows profile lease 源码与下一门槛

`core-go/platform/profile_lifecycle.go` 将加载、Environment 读写与停止串行化。stop 请求先阻止新操作，再等待正在执行的操作完成；只关闭自身子 key，随后释放本组件持有的 `LoadUserProfile` 引用。子 key 关闭、卸载或 token 关闭失败时保持停止态，下一次 `Close` 仅重试尚未完成的清理。加载已取得引用但后续路径核对失败时也保留清理能力。正常停止不删除已下发环境，不把交互登录/注销当作强制卸载全局 hive 的理由。

`NewWindowsProfileEnvironment(targetSID, trustedToken)` 是后续 profile broker 可复用的原生适配器，当前未接入正式 daemon。调用进程必须是 SYSTEM，线程不得正在 impersonate 客户端，既有 Backup/Restore privilege 必须已经启用。代码只检查，不取得登录 token、不启用/授予 privilege、不安装服务、不授予 ACL。Microsoft 要求 `LoadUserProfile` 调用进程为管理员或 SYSTEM，用户 token 具有查询、复制和 impersonation 访问权；本实现进一步限制为专用 SYSTEM owner。[LoadUserProfileW](https://learn.microsoft.com/en-us/windows/win32/api/userenv/nf-userenv-loaduserprofilew)

目标只支持已存在的本地 SAM 用户：精确比对 token SID、本机账号反查及重新解析，并拒绝已配置的漫游 profile。profile 路径来自该 token 的 `GetUserProfileDirectory`，与受管理员保护的固定 HKLM ProfileList 项交叉核对，不接收调用方 hive 路径。不用进程环境扩展路径；只允许从系统目录 API 得出的 `%SystemDrive%` 替换。拒绝 UNC、设备路径、ADS、路径歧义、目录 reparse point，以及其他用户可改写的权限描述符。加载期间还验证已有 `NTUSER.DAT` 的链接/权限元数据，不读取文件内容；目录句柄保持到清理结束，hive 文件元数据句柄在加载调用结束即关闭。[GetUserProfileDirectoryW](https://learn.microsoft.com/en-us/windows/win32/api/userenv/nf-userenv-getuserprofiledirectoryw)、[USER_INFO_3](https://learn.microsoft.com/en-us/windows/win32/api/lmaccess/ns-lmaccess-user_info_3)

加载得到的全权限 `hProfile` 不暴露给同步进程；只打开固定 `Environment` 子 key 的查询、写值及安全描述符查询句柄。用 `REG_OPTION_OPEN_LINK` 打开并拒绝 `REG_LINK`，避免跟随注册表链接跨 hive；子 key 不存在时失败，不自动新建。卸载仅调用 `UnloadUserProfile` 释放自己的引用，不直接 `RegCloseKey(hProfile)` 或 `RegUnLoadKey(HKU\SID)`。操作失败向上返回，不将 Session 0 通知视为交互会话已更新。[RegOpenKeyExW](https://learn.microsoft.com/en-us/windows/win32/api/winreg/nf-winreg-regopenkeyexw)、[UnloadUserProfile](https://learn.microsoft.com/en-us/windows/win32/api/userenv/nf-userenv-unloaduserprofile)

后续完整方案需要将同步/Vault 继续留在目标实例的虚拟服务 SID 下，另设不联网、不读取 Vault 的小型 SYSTEM broker。安装配置及二进制只能由管理员写入；broker 固定唯一 target SID，通过管道原生身份认证只接受对应虚拟服务 SID，不能接受任意目标 SID、路径、注册表根或用户 token。每个 target SID 只能有一个 broker owner，stop 必须等待在途操作并按上面的顺序释放句柄。当前没有该 broker、管道协议、安装器或无人登录 token provider，不能把此源码视为完整 Windows 支持。

`WTSQueryUserToken` 只返回已登录会话的 token，不能解决重启后无人登录；Microsoft 的 `KERB_S4U_LOGON` 路径另有域账号与权限前提，不能据此承诺本地、Microsoft Account 或 Entra 用户都可无密码启动。当前不猜未证明的 S4U 变体，也不离线改写用户 `NTUSER.DAT` 来替代 User Profile Service。[WTSQueryUserToken](https://learn.microsoft.com/en-us/windows/win32/api/wtsapi32/nf-wtsapi32-wtsqueryusertoken)、[LsaLogonUser](https://learn.microsoft.com/en-us/windows/win32/api/ntsecapi/nf-ntsecapi-lsalogonuser)

新增合成测试验证 stop 与读写并发、失败清理不复活、只释放自身引用、加载/子 key 打开失败、SID 错配和输入/路径拒绝；它们不调用 Windows API。Windows 交叉编译只检查 API 绑定，原生 ACL、profile 文件句柄与 User Profile Service 的兼容性、临时 profile 回退、登录/注销并发、SCM/Session 0 和重启无人登录均未跑。正式 Windows daemon gate 保持关闭。当前可见来宾执行工具仍阻塞；恢复正常 Windows 来宾执行能力后，先在隔离临时账号验证标准用户不能启动该适配器及原生身份/ACL，实际 SYSTEM profile 加载需一个可审查测试组件并通过工具审批。先完成受托 token 的加载/卸载验收，再独立验证无人登录 token 来源；不能把一次交互登录产生的 token 当作无人登录证据。

本轮实际结果：`mise exec -- go test -race ./platform -count=1 -v` 通过，新增 profile 合成测试 9/9，整个平台包 19 项顶层测试通过（1.921s）。`GOOS=windows GOARCH=amd64 CGO_ENABLED=0` 的平台测试交叉编译通过；Windows API 未执行。workspace 源码基础检查和差异空白检查通过。未创建测试服务、broker、账号或 VM，没有原生资源需要清理。

## macOS来宾最小待办

当前没有已验证的来宾执行入口。本机SSH配置只按UTM/macOS等明确别名只读检查，没有匹配项；没有跟随Includes、读取私钥或尝试用户真实认证，因此不能将其说成来宾SSH服务关闭。官方UTM queryIP返回0个地址，也不证明来宾没有地址。

继续原生LaunchDaemon验收需要提供一个正常入口：若已配置SSH，给出该来宾地址及专用合成测试账号/公钥入口；否则由用户自行在来宾或具备桌面工具的执行器正常建立并授权测试入口。不得擅自开启远程登录或辅助功能，不要求发送真实密码。macOS无人登录boot在取得入口后再验证，当前明确单列未验证，不无限阻挡Android、Windows及其他已授权开发。
