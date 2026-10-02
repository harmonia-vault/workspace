# 实现状态与验证

记录日期：2026-10-02（UTC）。这是阶段性结果，不代表完整端到端产品完成或生产安全验收。

## M0 已完成

五个 MIT public 仓库已建立，workspace 用 HTTPS submodule 固定其余四库提交。完整中文 [设计](DESIGN.md) 与 [执行计划](PLAN.md) 已保存，主 README 采用已确认的项目背景、目标和边界。

## M1 可测试安全底座

- protocol：确定性固定字符串数组签名编码、严格字段/版本校验、设备持钥会话挑战及三套 Go/Node 互操作向量。
- Go 密码学：独立环境钥匙、XChaCha20Poly1305 数据 AEAD、标准库 HPKE、独立 Ed25519 签名、严格 SHA256 密码派生、恢复用途分离基础。
- Go 本机协调：环境优先级、本机 override、首次逐 key 原值、删除/停用回退、暂停与收到撤销、离线期限、崩溃幂等恢复；CLI 可在显式合成 fixture 模式演示。
- Go 同步库：HTTPS、登录后单次持钥挑战换设备绑定会话、固定管理公钥验证、签授权/设备签名/HPKE/AEAD、持久检查点、历史补拉和已见写入序号绑定。
- server：Node SQLite 与 Workers 每账号 SQLite DO 共用 TypeScript 业务；D1 仅路由。逐请求当前授权、期限、代际、接受序号、幂等与签名；登录 Argon2id 64 MiB/3 次/并行度 1；测试入口不进入生产构建。
- 平台：原子 POSIX fragment、sh/bash/zsh 逐 key 恢复、暂停修订、Windows SID/registry/SCM 适配和三平台服务模板。生成模板不等于真实开机服务已验收。
- 手机：中文 Flutter 界面与显式合成内存预览；默认拒绝真实安全动作。Go 原生桥、钥匙保护和可信配对尚未接通。

## M1 固定提交的历史验证

| 检查 | 结果与范围 |
| --- | --- |
| core-go `mise run test` | 通过，M1 五个包、61 个顶层测试；CLI 7、cryptox 12、localstate 16、platform 7、syncclient 19 |
| core-go `mise run test-race` | 通过，同一 M1 源码的竞态检查 |
| core-go `mise run cross-compile` | 通过，macOS arm64、Linux amd64、Windows amd64；未发布二进制 |
| protocol `mise run test` | 通过，10/10；Go/Node 字节与 Ed25519 签名、域与编码篡改、最大数据包边界 |
| server `mise run check` | 通过，TypeScript 类型检查、Node 构建及 21/21 安全/业务测试 |
| 实际本地 workerd | 通过 DO SQLite、设备会话、序号、Argon2id 相同参数路径；不等于线上配额验收 |
| Docker 本机测试 | 镜像构建、临时持久卷重启、合成账号登录、非 root、目录 0700、拒绝远程明文监听通过；测试容器/卷已清理，镜像未发布 |
| workspace `mise run acceptance` | 通过：真实 Go 管理签/HPKE/AEAD → 测试 HTTPS → TS/SQLite → Go 固定钥验签解密；登录冒用、挑战重放、跨设备/账号、RO 写拒绝、幂等冲突、撤销与重授历史补拉 |
| macOS 交互 shell | 隔离临时目录 sh/bash/zsh 通过；未改宿主 shell |
| OrbStack Ubuntu ARM64 | 平台测试通过，zsh 缺失跳过；systemd 静态配置验证通过，未安装服务 |
| mobile `mise run analyze` | 通过，无分析问题 |
| mobile 控制层测试 | 9/9 通过；不包含 UI 单元测试 |
| Android 构建 | 尚未通过：第一次因中止缓慢 NDK 官方下载而结束，退出 130；需要完成官方 NDK 安装后重新构建 |
| Android 模拟器 | 隔离 AVD 曾实际 boot_completed=1；Mac 恢复后需重新核对进程与构建后验收 |
| 源码隐私/差异检查 | 基础扫描与人工范围检查通过，未使用真实账号/凭据；不等于完整秘密审计 |

各组件详细证据：[协议](https://github.com/harmonia-vault/protocol/blob/main/docs/TEST-EVIDENCE.md)、[服务端](https://github.com/harmonia-vault/server/blob/main/docs/TESTING.md)、[系统服务](SERVICES.md)、[手机](https://github.com/harmonia-vault/mobile/blob/main/docs/VALIDATION.md)。各仓库独立公开保存对应验证记录。

## 审查修正

合成复现发现并修复了登录会话冒用受信设备 ID、重新授权遗漏旧数据、到期/退出残留本机托管缓存、暂停 shell 仍纠正、全量历史重排已见签名写、Windows 原值类型不持久等问题。针对性回归通过；这不是生产安全审计。

## M1 固定提交时未完成的门槛（历史记录）

- 成熟 SPAKE2 配对、首台可信手机、完整管理授权链和历史接受时间证明。
- 受限恢复、完整新码重输、一次挑战和恢复公钥/所有封套原子切换。
- 邮件验证与邮件证明的破坏性账号重置，SMTP 真正握手/投递回归。
- 无人登录开机授权、本机 IPC、服务可读加密状态/密钥保护；三 OS boot/ACL/Windows hive/Session 0/Linux SSH 实机验收。
- Go 手机原生桥、系统设备密码/强生物认证、Android 真正构建/界面验收、iOS 构建。
- WebSocket 通知、Worker 线上 Argon2id 资源配额和容量/分页策略。

正式 enrollment、共享写入 CLI 和开机 daemon 入口仍保留拒绝执行的门槛。M1 只证明可测试底座，不代表“手机批准后自动配置凭据”的完整流程已可用。

M2 正在推进；以下为当前本机源码的实测结果，仍有在进行中的变更。先完成 M2，再按已授权计划实现自动更新及 CI/CD；不发布正式 Tag/Release/安装包或部署真实线上服务。

## M1 已公开的实现提交

protocol：`2c410a83178d99b87c966dd5fe6fbf56ca39943f`；core-go：`de91acb9753e9acdf89eb58069e130e70723675b`；server：`a60e269c94e7eea76e7e1ee0e7b76a15753e44a1`。M2 新包不包含在这些固定提交内。

## M2 当前验收与公开源码

当前公开固定提交：protocol `c3c902cb55bbf555380814a5dc074d07e3162f6f`、core-go `44e38371f8e4b836ed2c184ee0f39081c3a7480e`、mobile `c661894`、server `b7fcffd8a23301aaef997483f51d60337f2524c5`。workspace 用 submodule 保存这些源码位置；原生库、APK、真实凭据、私有测试目录不进入源码提交。多管理设备 v2、手机高层原生桥与 CLI 通知尚在本机整合，不能当成这些固定提交已完成的内容。

| 检查 | 实际结果与边界 |
| --- | --- |
| workspace 原生联合验收 | 通知依赖加入前 `mise run acceptance-native` 5/5 通过，5.414 秒。真实原生 SPAKE2、firstroot 双签、限时 RW 入网、已接受回应丢失的原加密回执恢复、无密码 boot、真实 HTTPS/TS/SQLite → 验签 Go → AEAD 状态 → 身份 IPC；环境创建/改名/完整轮换/暂停墓碑/全局撤销、完整恢复码重输与 nonce/新根及全部封套原子切换；手机 Go 首机、CRUD、未知结果重查、保存失败拒上传及撤销关闭 |
| 正式独立 CLI 与 daemon | 后续扩充的原生入网单项 3.911 秒通过（测试 3.48 秒）：删除本地登录 session，实际新 boot-session 200；正式编译 CLI 的 put/delete/仅选中 import、接受后 504 的原请求重查、历史重试不覆盖新值、本机 override 不上传、云删停止 override、暂停拒写及收到撤销清除；不使用 daemon fixture |
| server `mise run check` | 90/90 通过，类型检查和构建通过，任务约 14.03 秒/测试 13.59 秒；邮箱、boot、入网、恢复、生命周期、单次 WS 票据与逐次当前权限、纯序号提示、本人幂等收据和真实前端大请求边界 |
| SMTP TLS | 隔离证书的真实 TLS 握手 2/2 通过：强制 TLS、证书验证和拒绝降级；没有真实邮件投递 |
| Workers / Docker | 本地 workerd D1 仅邮箱目录、账号 SQLite DO 与 64 MiB/3 次/p1 Argon2 路径通过，首登录约 1387 ms；最新 Docker 构建与 Mac 恢复后隔离持久卷重启 smoke 通过，容器/卷已清理；Wrangler dry-run 247.41 KiB/gzip 61.05 KiB 通过，无真实部署 |
| workerd 关闭边界 | 到期 alarm 的真实 4003 关闭帧通过；Miniflare 代理 TCP FIN 延迟，曾导致标准 close 事件五秒超时。没有宣称 TCP FIN 或线上休眠/容量通过 |
| Go 回归 | 七包（cmd/harmonia、localipc、localkeys、localstate、platform、syncclient、mobileworkflow）最新 race 全通过。此前十包/原生标签回归及 darwin arm64、linux amd64/arm64、windows amd64 编译通过；不算 Windows 原生服务通过 |
| 独立 Ubuntu init 重启 | 新隔离 Ubuntu24.04 ARM64 内原生 SPAKE2 6/6、firstroot/真实入网/签名共享写后，删除登录 slot/引导输入并清旧服务端 session；只重启新来宾后 UID30001 无登录，新 boot/pull 200、无密码登录，正式 IPC/sh/第二 UID 拒绝、cap0、0700/0600 通过；本次账号/units/keys/SQLite 已清理，新机正常停机保留 |
| Linux 开机边界 | OrbStack 为 LXC，namespace boot ID 变化而内核 uptime 连续，因此只证明 init 重启。全局 LXC drop-in 关闭部分 systemd sandbox，未修改；物理内核开机、磁盘解锁和完整 VM sandbox 未跑 |
| UTM Windows/macOS | 既有两机正常启动；Windows 官方 guestexec OSStatus -10004、macOS exec 不支持，CUA transport closed。未改安全/登录设置；原生 boot/Session0/hive 未跑，Windows 正式 daemon 仍关闭 |
| Android 实际构建与 UI | 官方 NDK r28c SHA1 实际核对，Flutter APK 构建、隔离 API34 ARM64 AVD 安装启动通过；7 张真实合成预览截图已交付。首次 Maven TLS 短暂失败，自动重试通过；不使用宿主真实值 |
| Android 窄原生桥 | 实际 AVD 6/6：4 个 Go 原语/域/取消门槛，系统设备认证后的 AES 保护/重启公钥保持和独立再次认证，取消返回 AUTH_CANCELLED 且不写资料。测试 PIN/alias/files/独立测试包已清理。只证明窄桥，不等于首机/CRUD/配对/恢复高层已接通 |
| Flutter / iOS | 静态分析与 9/9 控制层测试通过，不含 UI 单元测试；已移除生成的 UI 测试目标。iOS project/scheme 解析通过，iOS 未构建验收 |
| 多管理设备证明 | 独立 Go `issuer-proof-v1` 7 项/29 子测、协议 Node14/14 通过，包含真实 C/D HPKE、A 历史 AEAD/签链与 archived v2 节点。现有正式单 pin 尚未自动升级，服务端/client/真实 PAKE v2 链正在接入；未宣称多管理手机闭环通过 |
| 当前整合检查 | 新 WS Go 依赖落盘后，workspace 全测曾在 setup 失败（顶层 go.sum 未同步），没有执行业务测试；等该切片冻结后补锁文件并重新验收。上述通过均保留实际执行范围 |
| 公开范围 | 基础源码秘密/个人路径/编译产物扫描与人工范围检查通过；只使用合成账号、临时 TLS 和独立 provider，无宿主真实 env/凭据。这不是完整秘密或生产安全审计 |

首次管理设备从独立 Ed25519/X25519、用途分离恢复钥和独立环境 HPKE 封套开始；初始化挑战绑定账号/代际/登录会话，设备与恢复钥双签后一次事务接受。配对使用固定 BoringSSL Edwards25519 SPAKE2 draft02 profile；短码只在端点使用。未链接成熟原生库默认拒绝，不声称 RFC9382 标准向量通过。

暂停独立拉取授权投影，执行撤销、到期和删除墓碑；恢复按原数据检查点补漏。创建/轮换须附齐当前设备和恢复封套，旧版本写入立即拒绝。最后环境删除须明确账号管理权限，当前关闭。退出先持久 AccountClosed/epoch 再清材料，崩溃重启不得复活旧设备；逐 key 恢复原值，保留无关修改。

仍需完成手机高层系统强认证、批准/恢复/轮换与多管理手机历史链、通知客户端重连补漏、三平台物理无人登录开机及 iOS。Worker 线上 Argon2 资源、容量/分页和完整安全审计未完成。先按可测试 M2 切片继续，再实现已授权自动更新与 CI；正式 Tag/Release/安装包、签名钥生成上传及真实线上部署仍未授权。当前不能宣传生产可用。
