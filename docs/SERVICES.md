# 本地系统服务与环境下发

当前为实验实现。已完成平台适配器、服务配置生成和合成数据测试；设备信任、机器保护密钥、真实同步及开机权限验收仍有门槛，不能宣传生产可用。测试没有导入宿主真实环境、凭据或账号，未在宿主安装服务、修改 shell 启动文件或注册表。

## 按本地用户隔离

一个设备上的每个本地用户都有独立实例、状态目录、激活排序、override 与恢复记录。daemon 参数必须包含明确 UID 或用户 SID，并在运行时校验实际身份；账号登录不能代替设备授权。

| 平台 | 服务配置 | 权限边界与尚需验证的项目 |
| --- | --- | --- |
| macOS | `LaunchDaemon`，标签包含 UID，`UserName` 为目标非 root 用户，`RunAtLoad`，`Umask=0077` | 管理员控制二进制和服务配置；目标用户只读写自己的状态目录。UID 与用户名对应关系必须校验。真实开机、无人登录、撤销和权限隔离尚未运行 |
| Linux | systemd 系统单元，目标非 root `User=`，`NoNewPrivileges`，空 capability，`ProtectSystem=strict`，`ProtectHome=true`，单独 `ReadWritePaths` | 生成器要求二进制在 `/usr/local/`，状态严格为 `/var/lib/harmonia/<UID>`，避免受保护 home 中的路径不可访问。真实开机、专用临时账号及 SSH 会话尚未验收 |
| Windows | 绑定目标用户 SID 的 `NT SERVICE\Harmonia-<SID摘要>` 虚拟服务身份；SCM 运行适配器支持停止和关机取消 | 只打开 `HKEY_USERS\<SID>\Environment`，禁止把服务身份的 HKCU 当目标账号。需要精确目录/注册表 ACL。目标用户 hive 未加载时立即失败；开机无人登录的 hive/profile 生命周期尚未实现 |

生成器只生成配置和清单，不安装服务、不授予 ACL。样例中的用户名、UID、SID 和目录均为虚构值。真实配置不含 `--fixture`；当前正常 daemon 因机器保护及可信同步未完成而拒绝启动。隔离测试必须显式使用 fixture，测试标记不能去除或作为可信注册捷径。

## 开机凭据存储的取舍

服务需要在用户登录前验证已有设备授权，不能把唯一解密钥放在只登录后才解锁的用户 Keychain 中。目标是按用户隔离的服务状态与经过验证的机器保护机制；该机制、权限边界和跨重启行为当前尚未实现，不应使用真实凭据验证。

启动前的磁盘解锁要求保留，不绕过 FileVault、LUKS 或 BitLocker。自动启动必然要求已启动系统能够使用某种机器保护材料；管理员、被攻陷的 root 或已经解锁并可读取保护材料的系统可能获得服务明文。磁盘加密主要保护未解锁的离线磁盘，不能承诺抵抗已控制系统的管理员。平台保护的软件钥匙不能统一宣称始终在硬件内。

POSIX 状态与片段使用专用 `0700` 目录和 `0600` 文件；写入通过临时文件、文件同步、原子替换与目录同步完成，拒绝非工具文件、符号链接和不私密的现有文件。Windows 的 Unix mode 位不能替代 ACL，实际 ACL 与 Windows 崩溃持久化仍需原生 VM 验证。片段包含供 shell 使用的值，属于本地敏感配置；当前 fixture 仅使用合成值。

## POSIX 原值与逐 key 下发

服务不能从外部改写已运行进程的环境。`POSIXProvider` 只保存下发目标，仅读显式提供的测试 baseline 或自身缓存，不枚举 `os.Environ`。engine 的 fixture baseline 与每个 shell 的原值是两层：每个 shell 首次 source 时只记录被接管 key 的存在性及原值，再应用下发值。

变量名只接受可移植的 shell 名称；内部 `__HARMONIA_` 命名空间保留。值以安全单引号字面量编码，包含引号、换行、`$()` 或反引号的合成值不会执行命令。空字符串与不存在分别记录。外部修改托管值时，正常刷新会纠正；无关变量和其他文件内容不变。shell 的只读内建变量不能强行改写。

最后一个来源消失时，片段输出逐 key release：恢复该 shell 记录的原值，或删除工具新增 key，并清除对应内部记录。不会把整份 shell 文件或整份环境快照恢复。其他环境仍提供同名 key 时，先回退到该来源。旧 shell 需要执行刷新才能观察更改；已经获得的明文不能追回。

暂停状态与每个 key 的本地修订号一起持久化。普通刷新每次纠正；暂停后，已见修订不再重复纠正，新的 shell 仍可一次应用保留配置。已收到撤销或到期造成来源变更时，新修订仍一次下发到剩余来源；全部来源消失时仍逐 key 恢复。暂停不屏蔽安全删除。片段状态先持久化再写片段，中途崩溃后重新打开会根据状态重建片段。

bash 使用 `PROMPT_COMMAND`，保留原来的字符串或数组内容；zsh 使用 `precmd` hook。sh 没有可移植的 prompt hook，当前只提供会话开始刷新与显式 `harmonia_refresh`；**sh 自动持续纠正尚未完成**。Linux SSH 的 shell hook 路径需要实际 SSH 会话验收，OrbStack 命令执行通过不能替代 SSH 测试。GUI、容器、cron 和其他业务服务的环境接入不在首版范围。

当前为保证老 shell 能执行 release，历史 release tombstone 与修订号不主动裁剪，可能随历史 key 数量增长；尚未设计可靠的清理边界。正常关闭 CLI 不清配置；停同步保留配置；崩溃后重开幂等收敛。真实卸载流程尚未实现，不能声称已完成系统清理。

## Windows 原值与通知

`WindowsProvider` 仅读取点名 key。Windows 名称不区分大小写，同批 `PATH`/`path` 等冲突会拒绝。原值记录包含不存在/存在、字面值、`REG_SZ` 或 `REG_EXPAND_SZ` 类型；不展开 `%NAME%` 引用。生产接入必须使用 `NewPersistentWindowsProvider`，原值在接管前落盘并绑定 SID，release 后清除记录，下次接管重新采集。只有内存的 `NewWindowsProvider` 用于隔离单次测试。

当前原生适配器只访问已加载的目标用户 hive，并发送 `WM_SETTINGCHANGE` 通知。Session 0 的广播不保证进入用户交互会话；新进程读取用户环境的完整生命周期仍需验收。已有进程的 env 不能被外部强制改写。不自动加载用户 profile，不偷偷要求备份/恢复特权，不写其他用户或系统环境。

## 本轮真实验证

| 验证 | 结果 | 证据与范围 |
| --- | --- | --- |
| macOS `go test ./platform -race -count=1 -v` | 通过 | sh/bash/zsh 字面量、纠正、逐 key 恢复；持久化重建；拒绝覆盖无关文件/符号链接；Windows 假存储跨重启恢复类型与二次接管；UID/SID/模板校验；bash prompt 数组保留；engine→实际 sh 暂停/撤销回退集成 |
| Windows amd64 平台测试交叉编译 | 通过 | `GOOS=windows GOARCH=amd64 go test -c ./platform`，只编译 SCM/registry 代码，未在 Windows 执行 |
| macOS `plutil -lint` | 通过 | 合成 LaunchDaemon plist 解析通过，未安装或启动 |
| OrbStack Ubuntu ARM64 平台测试 | 通过，zsh 跳过 | 临时目录、`env -i` 的 sh/bash 子进程和合成存储；VM 无 zsh；不改用户启动文件或现有环境 |
| Ubuntu `systemd-analyze verify` | 修复后通过 | 在临时 fake root 中放合成可执行占位与依赖 target，只验证配置，不执行服务。曾发现 `WorkingDirectory` 引号不被该 directive 解码，修复为绑定 UID 的绝对路径后复验通过 |
| 三平台真实开机服务、无人登录和权限隔离 | 未跑 | 尚缺可信同步/机器保护、Windows hive 生命周期与完整安装器；当前不能宣称完整平台支持 |
| Linux SSH、Windows Session 0/原生注册表、macOS 启动权限 | 未跑 | 需要专门 VM/临时账号验收 |

可复现命令：在 `core-go` 执行平台测试；在 Linux 执行 `sh service-templates/verify-linux.sh service-templates/examples/linux-harmonia-user-10001.service`。服务样例在 `core-go/service-templates/examples`。测试数据与输出没有真实凭据。
