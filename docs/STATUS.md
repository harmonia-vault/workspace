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
| server `mise run check` | 109/109 通过，类型检查和构建通过，任务 14.42 秒/测试 14.05 秒；新增 19 项 v2 Node/workerd 回归，包括 A→B→C→D 历史证明、精确当前 Admin 校验、撤销/到期/代际、恶意证明、SQL 原子回滚及大小边界 |
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

当前公开固定提交：protocol `fe67023bf917faa895fabcc439c40c82ad0a8e1a`、core-go `0787b9f663c4f11ab713e2e421554a23c421fac8`、mobile `01dadefb37e22d3a3ccebe4ac5ed26b53a3a4661`、server `e4b3cf5c278058b82382641152babb2af8783256`。workspace 用 submodule 保存这些源码位置；原生库、APK、真实凭据、私有测试目录不进入源码提交。这些提交包含首根手机业务、自撤销、审批桥、cert3 多管理入网、原初始化精确 genesis、完整环境来源和批量轮换确认。新固定源码 17 项整体验收实际失败，详见下表；不能以组件通过代替全链通过。此前完整 13/13 使用 core-go `1801392`、server `0d4d08e`、mobile `591d081`、protocol `bdf9e23` 独立快照，保留为原受限恢复基线；后续完整来源恢复和连续恢复授权仍在候选中。此前 8/8 使用 core-go `d89872a`、server `4fe2cb8` 与其余相同公开提交，保留为历史结果。
| 检查 | 实际结果与边界 |
| --- | --- |
| 前次 workspace 原生联合验收 | 此前公开固定源码的独立快照 `go test -tags harmonia_boringssl -count=1 -v ./acceptance` 8/8 通过，10.025 秒：首根/正式 CLI 2.28 秒、环境生命周期 0.81 秒、加密同步 0.58 秒、恢复 0.39 秒、手机 Go 工作流 0.74 秒、多管理设备 1.20 秒、通知 0.57 秒、自撤销五子项 3.01 秒。使用固定本地公开依赖和固定 BoringSSL，不复制账号/钥匙/状态；测试环境仅工具路径/缓存位置，临时快照已删除。此前 v2/通知切片 7/7 也通过 |
| 前次受限恢复固定源码完整验收 | 从公开 core-go `1801392`、server `0d4d08e`、mobile `591d081`、protocol `bdf9e23` 提取独立快照，原生全套 race 13/13 通过，30.845 秒；首根/正式 CLI、生命周期、加密同步、旧轮换、首根手机、v2 多管理、WSS、自撤销及新增受限恢复五主项全部实际执行。恢复单独定向 race 5 主项/17 子项通过，18.628 秒；含完整新码重输、原 ID unknown 查询、保存失败门槛、期限/壁钟回退、缺完整封套、fresh-context 缺来源及伪初始化双签拒绝。只传工具路径/缓存，BoringSSL 与依赖固定，两个临时源码快照均已清理；不包含尚未公开的 v3 Go/Android候选 |
| 新来源固定源码整合 | 上列四个公开提交的独立快照，加冻结新增三个测试文件，完整原生 race 实际 14/17 主项通过、3 主项失败，53.667 秒。两条 fresh-context 恢复负例均安全拒绝，但返回未知 `originHash` 字段或来源缺失的 403，未归入既有证据错误；另外旧手机丢响应夹具漏拦正式新端点、旧证书重启验证器未采用正式 daemon 的受保护来源升级入口。后两夹具修正后，同一公开快照定向 2/2 通过，6.002 秒；原 17 条恢复负例文件不改，整套尚未重新通过。新 cert3 来源纵链 4.58 秒、通用手机入网五个保存阶段 7.13 秒、非根创建/批准/轮换 5.61 秒、部分批次拒绝 1.37 秒在本次快照实际通过。快照仅工具/缓存环境，已清理；不包含后来候选恢复改动 |
| 显式进程扫描导入 | 新公开 core-go `731984a` 与其余相同公开提交的隔离源码快照，定向正式 CLI 纵链通过 2.964 秒（主项 2.52 秒）：只在完全合成 Env 的子进程列名，用户 select 后只取选中值；接受回应丢失后原 ID 查询仍序号 9，未选项不上传、暂停拒绝；保留 stdin 导入。此前当前源码同项也通过 2.787 秒。CLI 整包 race 7.702 秒、扫描定向 race 2.428 秒、vet、三平台默认构建和 Windows 测试编译通过；Windows 原生扫描未跑 |
| 正式独立 CLI 与 daemon | 删除本地登录 session 后实际新 boot-session 200；正式编译 CLI 的 put/delete/仅选中 import、接受后 504 的原请求重查、历史重试不覆盖新值、本机 override 不上传、云删停止 override、暂停拒写及收到撤销清除通过；v2 独立 daemon 从加密双签证书重建逐环境历史来源，RO 写拒绝，不使用 daemon fixture。v2 新验收曾两次因 SIGTERM 返回 context canceled 失败；修正取消退出后定向及 7/7 全测通过，持久化错误仍返回失败 |
| server `mise run check` | 最新公开完整恢复来源图 `e4b3cf5` 144/144 通过、0 失败/跳过，类型检查和构建通过，任务 15.68 秒/测试 15.294 秒；独立 Node/workerd 恢复图 10/10 通过，7.536 秒，本地 Argon2id 相同参数约 1475 ms。此前原初始化锚 134/134、19.27/18.8906 秒保留；完整 before/after 授权与身份、历史写入者来源、精确原 genesis、缺原记录拒绝、跨环境数据隔离和 SQLite 原子回滚均回归。此前 v2 109/109 记录保留在服务端文档；本次未重跑 Docker/Wrangler，也未部署 |
| SMTP TLS | 隔离证书的真实 TLS 握手 2/2 通过：强制 TLS、证书验证和拒绝降级；没有真实邮件投递 |
| Workers / Docker | 本地 workerd D1 仅邮箱目录、账号 SQLite DO 与 64 MiB/3 次/p1 Argon2 路径通过，首登录约 1387 ms；当前 v2 Wrangler dry-run 265.14 KiB/gzip 65.50 KiB 通过，无部署。2026-10-02 UTC 最新 v2 Docker 镜像重新构建及隔离持久卷重启 smoke 通过，验证合成空账号持久化、非 root、0700 和拒绝远程明文绑定；本次容器/卷已清理，未发布端口 |
| workerd 关闭边界 | 到期 alarm 的真实 4003 关闭帧通过；Miniflare 代理 TCP FIN 延迟，曾导致标准 close 事件五秒超时。没有宣称 TCP FIN 或线上休眠/容量通过 |
| Go 回归 | 首根审批 7 主项/27 子测及 native race 2.157 秒通过，十包普通 race 与三平台编译通过；统一手机错误码白名单后 mobileworkflow/syncclient/mobilebridge race 1.834/2.499/1.486 秒通过。SelfRevoke 两包 race 和真实 HTTPS 联合 race 通过。IPC 最新本包 race 3.051 秒、vet 和三平台测试编译通过；新增容量回归曾失败，实测 ENOTCONN 漏分类后修正，最终通过 |
| 最新 Go 来源回归 | 公开 `0787b9f` 本批冻结源码的默认全包 race、vet、三平台 CGO=0 构建通过；root 另以仅工具/缓存环境完整运行 `go test -race -tags harmonia_boringssl -count=1 ./...`，全部包通过，真实原生 SPAKE2 不跳过。cmd 7.806 秒、cryptox 2.268 秒、mobileworkflow 4.048 秒、syncclient 3.932 秒；协议 17/17、三份环境来源向量 SHA256 相同。手机高层真实 HTTPS 定向 race 3 主项/7 场景通过 15.396 秒，包括遗漏一条轮换内写入时数据检查点不前移、原 ID 全量恢复才标记已应用。该 Go 测试不能代替 Android 新构建验收 |
| 独立 Ubuntu init 重启 | 新隔离 Ubuntu24.04 ARM64 内原生 SPAKE2 6/6、firstroot/真实入网/签名共享写后，删除登录 slot/引导输入并清旧服务端 session；只重启新来宾后 UID30001 无登录，新 boot/pull 200、无密码登录，正式 IPC/sh/第二 UID 拒绝、cap0、0700/0600 通过；本次账号/units/keys/SQLite 已清理，新机正常停机保留 |
| Linux 开机边界 | OrbStack 为 LXC，namespace boot ID 变化而内核 uptime 连续，因此只证明 init 重启。全局 LXC drop-in 关闭部分 systemd sandbox，未修改；物理内核开机、磁盘解锁和完整 VM sandbox 未跑 |
| UTM Windows/macOS | 既有两机正常启动；Windows 官方 guestexec OSStatus -10004、macOS exec 不支持，CUA transport closed。未改安全/登录设置；原生 boot/Session0/hive 未跑，Windows 正式 daemon 仍关闭 |
| Android 实际构建与 UI | 官方 NDK r28c SHA1 实际核对，Flutter APK 构建、隔离 API34 ARM64 AVD 安装启动通过；7 张真实合成预览截图已交付。首次 Maven TLS 短暂失败，自动重试通过；不使用宿主真实值 |
| Android 窄原生桥 | 此前实际 AVD 窄桥 6/6 通过；最新同一 AAR/main/test APK 的完整原生 12/12 通过，184.013 秒、40 次设备密码提示含取消。覆盖真实注册/邮件证明/完整新码重输初始化、boot/pull、环境/变量 CRUD、原 ID 已接受 502 恢复、BUSY/保存失败/TLS 负例、known200 与 accepted502→status401/boot403 的自撤销；未知结果不虚报 completed，两个分支均清 alias/key/state。PIN、独立资料/包已清，预览与 AVD 保留；没有打开 Flutter 默认网关 |
| Flutter / iOS | 静态分析与 9/9 控制层测试通过，不含 UI 单元测试；已移除生成的 UI 测试目标。iOS project/scheme 解析通过，iOS 未构建验收 |
| 多管理设备证明 | Go 密码学 7 项/29 子测、Node 协议 14/14、v2 来源范围 6 项/21 子测及原生配对回归通过。真实 A→B Admin→v2 C RO，经 SPAKE2、B 本地根 pin 与逐环境证明、HPKE、已接受 504 原回执恢复和独立 daemon，验证 A/B 历史、RO 造密文拒写、B 降权立即拒写及 C 撤销清除。来源不依赖服务器目录或全局 Managers；该 v2 历史基线不包含新环境；新 cert3 来源纵链已实际通过，见新来源整合行 |
| 当前整合检查 | 加入 WS 依赖后顶层 go.sum 曾缺失而 setup 失败，已同步并通过后续 7/7 及最新独立快照 8/8。Go WSS→真正 Node/SQLite 的单次票据、断线持久序号补漏、暂停仅授权、恢复及 4003 后重查通过；通知不推进数据检查点 |
| 公开范围 | 基础源码秘密/个人路径/编译产物扫描与人工范围检查通过；只使用合成账号、临时 TLS 和独立 provider，无宿主真实 env/凭据。这不是完整秘密或生产安全审计 |

IPC 的受控复现使用真实 FileStore、原 1 秒预算/32 容量：旧实现排队 1119ms 后仍执行 override，响应头超时被误报协议错误。修复后请求约 1001ms 离开队列且未执行修改，已开始的 Save/fsync 仍完成并准确记录回复超时。原 20 并发全部成功断言保留，定向 race 三次通过，最大排队 278–286ms。此前三次失败没有原阶段记录，无法追认确切历史时序；超预算时仍可真实失败，不能把无回复当未执行。诊断默认关闭，只含固定类别与耗时。

首次管理设备从独立 Ed25519/X25519、用途分离恢复钥和独立环境 HPKE 封套开始；初始化挑战绑定账号/代际/登录会话，设备与恢复钥双签后一次事务接受。配对使用固定 BoringSSL Edwards25519 SPAKE2 draft02 profile；短码只在端点使用。未链接成熟原生库默认拒绝，不声称 RFC9382 标准向量通过。

暂停独立拉取授权投影，执行撤销、到期和删除墓碑；恢复按原数据检查点补漏。创建/轮换须附齐当前设备和恢复封套，旧版本写入立即拒绝。最后环境删除须明确账号管理权限，当前关闭。退出先持久 AccountClosed/epoch 再清材料，崩溃重启不得复活旧设备；逐 key 恢复原值，保留无关修改。

仍需完成手机批准/恢复/轮换的完整产品闭环、恢复连续授权及恢复后显式管理登记、既有设备角色/期限/撤销管理、暂停纯 KV 轮换保配置、三平台物理无人登录开机及 iOS。通知客户端重连补漏已通过上述真实联合验收，Windows 服务与通知联合实机仍未跑。Worker 线上 Argon2 资源、容量/分页和完整安全审计未完成。先按可测试 M2 切片继续，再实现已授权自动更新与 CI；正式 Tag/Release/安装包、签名钥生成上传及真实线上部署仍未授权。当前不能宣传生产可用。

## 正在关闭的安全门槛（不计为完成）

仅固定根设备公钥不能限制伪初始化授权。公开来源验证器现固定受保护原双签入网回执或初始化中精确的原初始授权集合，控制证据候选不能新增 genesis；真实非根新环境/轮换及已密封重启的 Go 联合主项通过。当前暂停收到纯 KV 轮换仍安全停用旧值，不满足完整“暂停保配置”，须把缓存数据来源和当前授权分别验证后再实现，不能仅放宽旧 KV 门槛。

公开 core-go `1801392` 已包含受限恢复切片：使用当前完整恢复码认证的精确原根，从已接受原初始化的完整 proposal、proof、设备与原恢复两签验证初始化承诺。当前恢复公钥可以因轮换改变，原初始化记录不改变，只有原 proposal 精确承诺的初始授权成为 genesis；缺记录、nonce/签名篡改、重新计算 proposal hash、根设备真实签一个新 Y 授权后伪装初始化均拒绝。此业务代码针对当时服务端候选的真实 HTTPS/SQLite race 验收实际 5 主项/17 子项通过（18.626 秒）。现在对应服务端已公开 `0d4d08e`，相同四子仓公开固定源码的独立恢复 race 18.628 秒和完整 13/13 race 30.845 秒均通过，含新增/轮换后的 fresh-context 来源缺失负例。对应组件 race 及隔离 core-go `731984a` 加仅恢复六文件的独立包 race 也通过。

恢复切片只恢复已验证的初始环境，新增环境/跨 keyVersion 尚须完整来源图，缺失时安全拒绝。恢复码轮换接受后仍受限，不自动信任新设备；可验证恢复授权连续链及显式新管理手机登记尚未实现，Android 新锚恢复未跑。

Android 首根→CLI v2 审批在此前编译基线（core-go `c4dec971`，working tree modified）集中 1/1 通过 59.074 秒，mise 入口复测 58.733 秒；四个 CLI 控制器成功，覆盖 unknown 原 ID 查询及两次保存失败无批准 POST。当前 origin-aware CLI 显式 certificate-version 2 重跑 55.248 秒和诊断 54.702 秒均失败：known 分支通过，第二候选完成双签入网但 Pull 拒绝，daemon/IPC 存活，公开状态为 0/0/0。服务端完整控制证据闭包已补并公开，Go 校验不放宽。随后唯一 fresh focused 使用新正常 AAR 和显式 v2 CLI（`1801392+dirty` 构建标记，非公开固定源码产物），实际 1/1 通过 59.634 秒，21 次系统认证含 1 次取消，四个控制器退出 0；known、丢响应原 ID 确认、两同步保存失败无批准 POST、取消/BUSY、Logout 全部执行。CLI/provider、PIN、两测试包/槽/文件、forward、HTTPS/SQLite 已清理，原 preview 与 AVD 保留。后续源码变更没有冒称已由该产物测试，固定源码 Android/v3/恢复仍待后续验收。此前 12/12 的范围仍不扩大为远程批准或恢复通过。

本轮 Mac 执行器只读确认 UTM Windows/macOS 均 started，但可用工具列表没有来宾 CUA/桌面能力；未重复失败 guestexec、未改虚拟机安全配置。父线程中途消息工具在能力刷新后不可用，本机与 GitHub 推送正常；本文件保存可复查结果，不以其它外部通知代替线程回报。
