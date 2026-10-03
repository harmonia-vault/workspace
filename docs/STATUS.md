# 实现状态与验证

记录日期：2026-10-03（UTC）。这是阶段性结果，不代表完整端到端产品完成或生产安全验收。按用户功能分类的完成/进行/未开始/受阻清单见 [TODO.md](TODO.md)。

## M2 用户流程总览

2026-10-03 当前 workspace submodule 固定 protocol `de21d99`、core `b094a93`（原生账号、恢复、独立PIN包装及同锁来源投影）、mobile `9e0a00b`（C界面实际预览、Android独立PIN适配及iOS安全桥）、server `bd86fec`（注册策略、空实例测试入口和待批准元数据）。公开核心联合39项有两个精确固定快照，另列真实Android和平台产物范围；新Flutter产品接线及视觉迭代仍在进行，不将组件证据合并冒称完整产品通过。

| 用户流程 | 完成及实际证据 | 待验 / 阻塞 |
| --- | --- | --- |
| 注册、邮箱验证、首机新码完整重输、环境/变量CRUD | 真实Android注册、邮箱证明、退出及普通登录未可信通过；完整新码重输、原生初始化/会话恢复/未决查询完成，Flutter最后一次Pull待验 | 该实际链仍使用旧界面；脚本少回应最后一次Pull的系统认证，整体PARTIAL，完整首页/CRUD/CLI未跑。最终C两文件新组合analyze及68项通过，当前公开核心新AAR已构建，完整C链继续；真实外部邮件投递未跑 |
| 手机批准CLI及本机环境同步 | 正式cert3 CLI/daemon、原生PAKE、HPKE/AEAD、RO/RW、selected-only导入、override、暂停和撤销已实际通过；Android cert3 focused19次CryptoObject通过 | Flutter授权入口已接，完整实际用户链未验；不把编译当三OS boot |
| 恢复后手机入网并批准CLI | Go完整旧码→受限→新码重输/连续两签→明确env/role/expiry cert4登记→Boot/Pull→CLI4 PAKE/原receipt504恢复→CGO0 daemon及RO拒写/RW写删已通过；源码`b28c46a`，新两验收文件公开 | 真实Android恢复3/3、30次强认证及两次force-stop已通过，22文件公开；Flutter向导仍在接线，第二次恢复DAG仅密码学库已验、HTTP与产品未接 |
| 手机设备权限管理 | Go已有设备RO/RW/Admin/none、全局撤销、原包未知查询及保存失败门槛通过；Android新focused1/1、42次CryptoObject、真实Go peer通过 | 当前已通过管理Android产物是固定core894等公开底座+9冻结候选，不等于最新HEAD；恢复E/F低层控制已公开及真实Go验收通过，高层环境日志已公开并经真实HTTPS候选测试；Android及手机界面接线继续 |
| Linux后台无登录 | 原OrbStack隔离Boot/Pull/IPC/sh/SSH通过；新完整Ubuntu内核/systemd、cloud-init、离线固定主机公钥匹配SSH、独立数据卷和固定工具链通过；当前公开CLI/daemon及server在guest构建通过 | 正式systemd unit、最小权限隔离和整机重启无人登录Boot/Pull仍待验；不以构建或LXC结果替代 |
| macOS后台无登录 | LaunchDaemon/目标UID/受保护状态/CLI及隔离shell组件通过；Computer Use权限及精确UTM窗口控制实际可用 | 旧VM已依明确授权停止并保留。新独立macOS VM使用已有官方镜像安装启动通过，许可已获授权接受，合成本地账号Setup继续中；正常桌面及真实LaunchDaemon boot尚未验证 |
| Windows后台无登录 | SID/DPAPI/SCM组件及原生配对通过；上一轮严格未知结果处理、cleanup和独立absence通过；新轮实际同一注册会话的连接SID匹配目标普通用户、目标服务器匹配本机 | 原flags2注册仍80020009/SCODE80070005；同目录目标允许mask1201bf且无deny，根因仍未证实。没有重试/启动服务或扩大ACL；真实manifest只读回收中，正式CLI/SCM/provider/boot未验 |
| SMTP / Workers | 隔离SMTP严格TLS实际2/2通过；本地workerd/SQLite DO及相同Argon2id64MiB/t3/p1通过，公开server183/183+type/build通过 | 真实投递、线上CF配额为外部验证待办；未授权线上部署，不为额度弱化参数，也不无限阻挡其余已授权开发 |

最近独立完整公开 snapshot：workspace `2ba0c1a`、core `457858f`、server `b968223`、protocol `b770001`、mobile `d169d74`，原生 race 33/33 通过161.661秒；同一固定源码 Go 全包293项通过、1项默认构建专属反向检查跳过，12个含测试包全部通过。仅提取公开 commit，无 working-tree overlay。该结果已包含连续恢复及正式CLI4新6主项，不包含后续恢复E/F管理、Android恢复或产品UI候选。其后纯公开 workspace `f1d10b1`/core `202a83c`/server `02689b4`/protocol `2edad19`/mobile `d169d74` 的5项新增或受影响用户流程 race 通过69.442秒；无工作树覆盖、0失败/跳过。该固定快照共有37主项，本轮没有将5项定向复验说成完整37项通过。新增高层环境业务在公开9eb77df/core006450d独立target1/1通过13.497秒；随后严格挑战时钟验收公开后，root仅提取五仓公开固定源码完成39/39主项原生race（247.939秒、0FAIL/SKIP）；同一core全包307 PASS、0FAIL、1默认构建专属SKIP；精确范围见下表。M2仍未闭环；CI、Tag/Release、安装包、签名钥生成/上传和真实线上部署均未执行。

## 最新实际进展（2026-10-03 09:43 UTC）

- Android真实注册→邮箱证明→退出→普通登录未可信通过。完整新恢复码重输、原生初始化、restoreSession和businessPendingInfo已完成；旧驱动只回应此段前三次系统认证，环境标题提前显示，但最后Flutter Pull的第四次认证未回应，故初始化整体为PARTIAL，完整首页/CRUD/CLI未跑。累计服务端Boot/Pull成功响应不能归为最后Flutter Pull通过。13个不同CryptoObject窗口、原报告和尾部失败保留；每轮官方清除合成系统PIN及独立SDK noSecure通过。前一文档提交误将此段整体算通过，本项明确更正。
- 已核实上述实际账号链使用旧界面快照，不是最终C界面。业务lib与当前公开底座逐文件相同，仅两UI文件不同。新独立候选只换入最终公开C两文件，analyze通过3.529秒、既有68项通过4.725秒；完整C用户链另验。当前公开core `b094a93`新AAR构建5.558秒通过，root独立对照公开archive的350源码文件零差异，APK产物和旧证据分开记。
- PIN产品安全审查修复了服务器可能已接受后的原ID保留、状态故障关闭明文与业务能力、prepare后晚注册owner与dispose竞态，以及忘记PIN必须只清PIN所属材料。v2非视觉候选35项定向测试、确定性JVM交错及完整App编译通过；这是候选与合成故障端口证据，真实PIN完整用户链未跑。一次本机Claude Opus5.5 medium已完成两文件UI；机械清理修正后analyze和既有68项通过。所有PIN产品能力仍默认关闭，候选未公开为已可用。
- 新独立macOS VM使用本机已有官方恢复镜像安装至100%并启动。已接受本次获授权的Apple许可，语言、地区、新机和隐私步骤通过，合成本地测试账号设置继续中，尚未确认进入正常桌面。旧VM按明确授权停止，磁盘及配置保留；无真实Apple ID或宿主权限变更。
- Linux完整VM已证明正确NoCloud启动与全部cloud-init阶段无错误；离线白名单取得的公开主机指纹与实际SSH一致，严格SSH通过。任务独有16GiB空盘经核验后一次格式化、UUID挂载及0700工作目录通过。root空间门槛和npm配置的首次失败保留；将新VM公开apt索引迁至数据盘后固定Go/Node/pnpm和编译工具安装通过。公开固定core/server/workspace在guest低并发构建CLI、daemon及server通过169.045秒，正式服务和整机boot仍待验。
- Windows上一轮严格cleanup及独立absence通过。新的单次真实注册同一会话诊断确认ConnectedUser解析SID与目标相同、TargetServer匹配本机，目录目标ACE允许1201bf、deny为0、无继承标志；flags2实际仍返回SCODE80070005。仅记录实际证据，VARIANT布局仍只是源码预测，未声称原生捕获。尚未找到根因，未启动服务、扩权限或继续盲试参数。
- iOS平台桥已公开为mobile `9e0a00b`：Simulator与未签名iPhoneOS arm64完整构建通过；独立实际安全15 PASS/0 FAIL/3 UNRUN，产品实际安装、启动和连接页查看通过。真机认证、实际NO_SYSTEM_AUTH完整PIN及可信产品链仍未验；后续官方Simulator认证验证排在macOS Setup之后。

以下带时间段为历史；上述为当前结果。M2仍未闭环，不宣称生产可用，未配置CI、发布Release/安装包或部署真实线上服务。

## 本轮实际进展（2026-10-03 08:26 UTC）

- mobile `dd487eb`公开9个精确文件：8个已审PIN适配/测试/脱敏证据，加C界面实际验收文档。root逐文件摘要与公开源码扫描通过；没有公开AAR/APK、密钥或本机日志。
- 实际Flutter C浅色/深色共12张原始截图通过，涵盖连接、环境列表、Admin/RO详情、设备和设置。第一轮脚本焦点检测失败保留，第二轮仅修检测后通过；临时包卸载、171包名基线、主题恢复、本人模拟器退出均通过。截图只含纯内存合成数据，已保存Library。
- PIN原JNI5/5保持原源码范围；另从公开mobile `a06acaf`加同8文件及候选AAR进行完整App编译，35秒通过，134源文件摘要未变化。三个私有构建准备失败保留。插件、Kotlin PIN类及Go LocalPINCore实际进入DEX/JNI产物；此构建未安装，PIN产品入口仍关闭。
- Android真实账号链仍受系统锁屏测试驱动阻塞。多轮尝试的代理完成响应计数为0（不据此断言无网络尝试），退出时实际SDK确认未残留系统锁屏。按要求停止反复猜选择器，先完成无需强认证的合法段，并比对既有成功认证脚本；不修改产品认证规则。截图诊断曾被自动审批拒绝，已使用不读取敏感输入的公开页面标识替代。
- Windows修正旧探针的安装文件名遗漏，重新实际确认本次installed进程0、无查询失败、两服务Stopped、任务目录空。严格处理未知结果及实际manifest只三字段变化读回通过；同源精确cleanup返回PASS，独立资源absence尚在核验。历史数值未知与前轮ACCESS_DENIED保留，不将旧错误筛选的process0当作完整证明。
- Linux新VM真实Ubuntu24.04.5、Linux6.8 aarch64及systemd多用户目标启动已观察；180秒串口捕获未等到cloud-init/host指纹，网络等待失败。因此没有SSH、格式化或Harmonia安装。正在核对seed呈现，不能将kernel已启动说成无人登录服务通过。
- iOS官方运行时下载完成，Go/BoringSSL的设备与模拟器slice和完整两个架构App构建通过；未签名、未使用开发者账号。新模拟器安全harness首轮超时，已定位测试host缺Scene生命周期；修正后再验。Swift平台源码审阅发现PIN关闭错误与早期失败清理需要修正，能力仍关闭。

M2仍未闭环。下一步优先可实际操作的最小用户流程，受阻认证段明确列出最小环境操作；其他平台继续。未配置CI、发布Release或安装包、部署真实线上服务，不宣称生产可用。下方带时间的段落保留当时状态，以上为最新增量。

## 07:52 UTC 增量与当前用户路径

- 用户已正式选定C砂岩明暗风格并接受整项圆角底高亮导航。共享Flutter源码与受控原生接线17文件已公开为mobile `a06acaf`；最终分析通过、既有68测试通过，独立debug预览APK实际构建通过10.649秒。HTML明暗导航示例仅为设计图；最终Flutter运行截图和完整业务用户链尚待验。
- 真实Android产品夹具已完成实际服务地址输入→严格HTTPS实例合同检查→默认注册页。五次测试输入定位失败完整保留，最终通过使用标准SDK输入/焦点操作，未绕过产品按钮门槛。账户阶段随后在系统屏幕锁设置测试夹具选择器处失败，注册/邮箱验证/普通登录尚未跑到，初始化/CRUD/CLI仍未跑；只读屏幕锁状态与清理读回正在核验。不能将组件68项通过当作用户链通过。
- 独立PIN新实际Go JNI5/5通过（2.286秒，完整23.704秒），真实系统无认证能力分类/正常权限、错误PIN持久计数、并发锁、Keychain替代的AndroidKeystore MAC与AtomicFile/CAS、忘记PIN新钥和早期失败清理均覆盖。两次模拟器路径准备失败保留；实际测试包已卸载，包基线恢复，新测试AVD已停。此8文件切片待公开，Plugin/UI能力仍关闭，正确PIN仅返回未可信，不等于批准CLI。
- Go同锁来源投影三文件公开为core `b094a93`。root仅从公开底座及冻结三文件独立race通过8主/14子项（19叶场景），9.204秒；owner相关两包133项通过。首机、V3、V4来源重新验签、期限、未知原请求和最后保存门槛保留；新Android来源ABI及冷恢复审批尚未开启。
- Windows1007精确资源清理及独立absence通过；注册80070005历史保留。新1008进程内COM设置实验失败，只实际确认普通Batch/Users/非Admin/Session0，后续API到达尚无证据。正式CLI/SCM/provider/无人登录开机未通过，不扩系统权限或改ACL。
- Linux新独立bundle材料经过官方镜像签名和修正ISO填充核验，通过。正常UTM import已创建持久managed VM并保持stopped；多余save脚本命令不受SDEF支持而失败，既有VM配置及状态未变。首次完整kernel boot/SSH/systemd隔离仍未跑。
- 用户将iOS改为当前并行范围。独立平台owner已启动：Xcode27.0与iOS/Simulator27.0 SDK存在，Simulator runtime/device为空；现iOS仅Flutter注册脚手架，Go/Keychain/LocalAuthentication平台桥尚未实现。官方runtime及独立测试模拟器准备中，不购买开发者计划或发布安装包，不把Simulator等同真机认证验收。

全部源码公开前基础秘密扫描通过，仅源码、脱敏文档及合成测试公开。M2仍未闭环，CI、Release/安装包及真实线上部署未执行；当前不能宣传生产可用。

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

本轮增量公开提交：protocol `2edad19`、core-go `006450d`、server `02689b4`、mobile `d169d74`。Android v3 源码已核对实际产物范围并公开；其 AAR 验收仍以旧公开底座加10个冻结文件为界，不冒称最新 core 全树产物。恢复封套与设备管理已公开，连续恢复协议、服务及Go/CLI4链已公开，Android恢复与真实Flutter产品链仍在接线。workspace `b96c18b` 的第一次独立快照因暂停测试依赖尚未提交的管理接口而编译失败，测试未执行；本次补齐 core 固定依赖并保留失败，不放宽断言。

前次完整来源交付的固定提交：protocol `fe67023bf917faa895fabcc439c40c82ad0a8e1a`、core-go `0787b9f663c4f11ab713e2e421554a23c421fac8`、mobile `01dadefb37e22d3a3ccebe4ac5ed26b53a3a4661`、server `e4b3cf5c278058b82382641152babb2af8783256`。workspace 用 submodule 保存这些源码位置；原生库、APK、真实凭据、私有测试目录不进入源码提交。这些提交包含首根手机业务、自撤销、审批桥、cert3 多管理入网、原初始化精确 genesis、完整环境来源和批量轮换确认。前次新固定源码 17 项整体验收实际失败保留，现本轮25项完整公开快照已通过，详见下表；该结果不扩大为后续连续恢复或手机UI已完成。此前完整 13/13 使用 core-go `1801392`、server `0d4d08e`、mobile `591d081`、protocol `bdf9e23` 独立快照，保留为原受限恢复基线；后续完整来源恢复和连续恢复授权仍在候选中。此前 8/8 使用 core-go `d89872a`、server `4fe2cb8` 与其余相同公开提交，保留为历史结果。
| 检查 | 实际结果与边界 |
| --- | --- |
| 前次 workspace 原生联合验收 | 此前公开固定源码的独立快照 `go test -tags harmonia_boringssl -count=1 -v ./acceptance` 8/8 通过，10.025 秒：首根/正式 CLI 2.28 秒、环境生命周期 0.81 秒、加密同步 0.58 秒、恢复 0.39 秒、手机 Go 工作流 0.74 秒、多管理设备 1.20 秒、通知 0.57 秒、自撤销五子项 3.01 秒。使用固定本地公开依赖和固定 BoringSSL，不复制账号/钥匙/状态；测试环境仅工具路径/缓存位置，临时快照已删除。此前 v2/通知切片 7/7 也通过 |
| 前次受限恢复固定源码完整验收 | 从公开 core-go `1801392`、server `0d4d08e`、mobile `591d081`、protocol `bdf9e23` 提取独立快照，原生全套 race 13/13 通过，30.845 秒；首根/正式 CLI、生命周期、加密同步、旧轮换、首根手机、v2 多管理、WSS、自撤销及新增受限恢复五主项全部实际执行。恢复单独定向 race 5 主项/17 子项通过，18.628 秒；含完整新码重输、原 ID unknown 查询、保存失败门槛、期限/壁钟回退、缺完整封套、fresh-context 缺来源及伪初始化双签拒绝。只传工具路径/缓存，BoringSSL 与依赖固定，两个临时源码快照均已清理；不包含尚未公开的 v3 Go/Android候选 |
| 新来源固定源码整合 | 上列四个公开提交的独立快照，加冻结新增三个测试文件，完整原生 race 实际 14/17 主项通过、3 主项失败，53.667 秒。两条 fresh-context 恢复负例均安全拒绝，但返回未知 `originHash` 字段或来源缺失的 403，未归入既有证据错误；另外旧手机丢响应夹具漏拦正式新端点、旧证书重启验证器未采用正式 daemon 的受保护来源升级入口。后两夹具修正后，同一公开快照定向 2/2 通过，6.002 秒；原 17 条恢复负例文件不改，本次组合保留为失败记录；后续完整固定25项通过见新增行。新 cert3 来源纵链 4.58 秒、通用手机入网五个保存阶段 7.13 秒、非根创建/批准/轮换 5.61 秒、部分批次拒绝 1.37 秒在本次快照实际通过。快照仅工具/缓存环境，已清理；不包含后来候选恢复改动 |
| 本轮完整公开快照联合验收 | 仅从公开 workspace `8e2787ae239bc85073e657eb7a7b2e3a4521390c`、core-go `437f3ebc210571815c3f42c30a08f7936e5ba0ae`、server `e1572d646afd6fc46fc8c7846431ddfef1a47464`、protocol `c0d3df4a79a57781178c02487e8105b5cd3d5896`、mobile `01dadefb37e22d3a3ccebe4ac5ed26b53a3a4661` 提取独立快照，完整 `go test -race -tags harmonia_boringssl -count=1 -v ./acceptance` 25/25 主项通过，110.978 秒、exit 0。无 working-tree overlay；原 17 条恢复文件逐字保留，包含先验签后 HPKE、fresh-origin 错误归类、全量批次新/已缓存两反例、改权撤销、原生 cert3/正式 daemon、暂停四安全场景。真实 expiry 子项等待15.61秒；25项含管理8.81秒、恢复完整12篡改3.86秒、暂停22.72秒。只传工具路径/缓存，所有账号/环境/TLS合成；快照已删除。不含后续连续恢复服务、新恢复设备或 Android UI产品接线 |
| Android cert3 新原生闭环 | core-go `a038275`、mobile `3b72a29` 已公开；19冻结文件逐项SHA核对，实际10编译候选和runtime snapshot逐字相同。独立正常AAR/主-test APK、默认3 CLI及两个控制器成功；真实AVD focused 1/1通过52.072秒、19次系统强认证、两controller exit0。B仅Y Admin入网，finalApplied保存#4失败关闭并原receipt恢复；Y轮换；B批准C限时RW仅Y；C双签/boot/拉取/导出/写成功，X读写/激活拒绝；accepted502原ID确认与Logout通过。第一次13.476秒因测试Admin枚举大小写停止并保留，重建后才全通过，v2批准计数0。PIN/测试包/forward/CLI/HTTPS清理，原preview与AVD保留；UI/defaultgateway与整体ready仍关闭。精确源/产物及边界见核心mobilebridge/V3_NATIVE.md |
| 新原生桥与恢复管理同树回归 | root仅从公开 core-go `a038275`、server `e1572d6`、protocol `c0d3df4`、mobile `3b72a29` 和 workspace `fa9d815` 提取独立快照，完整 core `go test -race -tags harmonia_boringssl -count=1 -v ./...` exit0，12个含测试包均通过；cmd8.106秒、syncclient6.394秒。native标签下默认构建专属逆向测试明确SKIP，不计为已执行通过；原生SPAKE2路径实际执行。此快照不含其后cde5120分支修复或连续恢复草稿 |
| 额外局部审查与分支修复 | `cde5120`已公开：真实管理故障代理先复现三个错误Applied成功（device_untrusted/任意403/仍Admin），修复后五子项race通过4.75秒；只有精确admin_required且当前已验非Admin可作为自降权例外，失效先清信任，finalSave失败Applied=false。原管理链新增POST前Close/New后仍以原token/hash重试，race通过9.78秒。纯时钟回退先unit失败0.471秒（clock回正恢复明文）；改为SessionClosed/清token/keys/值事件/关owner+保存，新二场景及原七场景真实HTTPS race通过13.416秒，回正和sealed reopen仍拒。保存失败不能称已持久失效，原生registry须同步关闭；这不是完整安全审计。root从公开workspace `64e0aef`、core `894f2ad`、server `e1572d6`、protocol `b770001`、mobile `3b72a29` 独立提取快照，三个定向主项全部race通过17.950秒、exit0：末管理五故障4.80秒、原管理跨对象原签撤销9.39秒、纯回退两项2.38秒，无working-tree覆盖；临时快照已删除 |
| 已缓存写入仍须完整全量批次 | 公开 `0787b9f` 加冻结新反例实际失败 1.897 秒：全量遗漏已有 Seen 指纹的轮换内写入仍被接受。core-go `44a11bd` 修复为全量仅使用本次响应实际验过的序号/签字指纹；同一固定公开底座只叠此九行修复及冻结测试，两项真实 HTTPS race 通过 4.515 秒，完整 syncclient native race 通过 3.842 秒。反例不弱化，源 overlay SHA 与固定基线可复查；本轮完整公开快照已通过 |
| 暂停保留旧配置 | core-go `220cf5f` 已公开：缓存数据来源和当前授权分开，连续已签 KV/GG 链保留旧值，角色/期限按链中最小上限；授权序号前移而数据序号和已见写入不前移。原生 HTTPS/加密重启/恢复新 KV 通过 3.38 秒；撤销、删除、到期、权限上限四场景通过 19.50 秒；全部 Go race 及最新受影响两包通过。独立 workspace 固定测试现随25项完整快照通过 |
| 恢复封套临界修复 | 服务端 `e1572d6` 已公开完整 signed Change 与原十三域轮换清单的同事务投影，160/160 通过、0 失败/跳过，类型检查/构建通过，17.45 秒。候选 Go full-origin 保留原根/精确 genesis，真实空环境任意 HPKE 替包曾失败 1.647 秒；签名承诺修复后同反例通过 1.26 秒，旧 17 及新增恢复合计 9 主项/25 场景通过 37.833 秒，另完整包 12 篡改场景通过 5.403 秒。core `887f932` 已公开签名验证先于 HPKE；顺序负例 race 1.498 秒通过，真实 fullpacket/合法恢复/empty 三主14场景 race 14.796 秒通过。本轮完整固定来源回归已通过。当前仍受限，不自动设备可信 |
| 既有设备管理切片 | 服务端 `f5adbed` 的管理投影、原 ID grant/revoke 状态收据已公开，153/153 通过。`e1572d6` 补齐 none/expired/旧 KV 的历史签发者证明，不开放当前权限或返回数据；候选 Go 真实 HTTPS 管理流程通过：选定角色/期限、原包受保护保存、丢回应/重启只查原 ID、None/到期/重新授权、全局撤销及旧 boot/session 失效。core `887f932` 已公开；late status/POST 撤销重置会先持久清信任和待处理。最终两包 race 5.882/2.806 秒、真实原生联合 10.05 秒通过，Android 管理页仍未接 |
| 显式进程扫描导入 | 新公开 core-go `731984a` 与其余相同公开提交的隔离源码快照，定向正式 CLI 纵链通过 2.964 秒（主项 2.52 秒）：只在完全合成 Env 的子进程列名，用户 select 后只取选中值；接受回应丢失后原 ID 查询仍序号 9，未选项不上传、暂停拒绝；保留 stdin 导入。此前当前源码同项也通过 2.787 秒。CLI 整包 race 7.702 秒、扫描定向 race 2.428 秒、vet、三平台默认构建和 Windows 测试编译通过；Windows 原生扫描未跑 |
| 正式独立 CLI 与 daemon | 删除本地登录 session 后实际新 boot-session 200；正式编译 CLI 的 put/delete/仅选中 import、接受后 504 的原请求重查、历史重试不覆盖新值、本机 override 不上传、云删停止 override、暂停拒写及收到撤销清除通过；v2 独立 daemon 从加密双签证书重建逐环境历史来源，RO 写拒绝，不使用 daemon fixture。v2 新验收曾两次因 SIGTERM 返回 context canceled 失败；修正取消退出后定向及 7/7 全测通过，持久化错误仍返回失败 |
| 第二次独立公开39项完整回归 | root仅gitarchive公开 workspace `8b84acbe08420d5922b26b2d2f4f310838f0a892`、core `5040921e1867578be291b985736811326ed8ae6d`、mobile `56ac910d4c1778083d94fe2091a84392aa603362`、server `909d4988c8de7436a648623b2b852ec69bff41ad`、protocol `de21d9907dd7b73636afeabee4806655aaa0a82e`。完整原生race 39/39主项PASS、0FAIL/SKIP、251.752秒、exit0，无working-tree overlay且快照已删。包含公开恢复组件及新注册服务兼容性，不包含后来的PIN、待批准路由、Windows候选或Flutter真实账号链 |
| 新注册公开server独立完整检查 | root独立公开 `909d4988c8de7436a648623b2b852ec69bff41ad` 无overlay，typecheck1.397秒/build0.847秒均exit0；245/245 PASS、0FAIL/SKIP/cancel，进程32.645秒/测试32.523213秒，本地workerd Argon同参数1649ms。快照已删，不是线上配额或实际手机界面验收 |
| 待批准请求元数据 | server `bd86fec6215b6f7149234578e7bf5dc764a639c2` 九个冻结源文件公开；新增Node/workerd定向10/10 PASS，完整255/255及typecheck/build PASS，任务38.86秒/测试38.509秒。root另仅public archive独立定向10/10 PASS、0FAIL/SKIP/cancel，typecheck1.234秒/build0.722秒/测试进程3.760秒（实际测试3.657秒）。每请求重查当前Admin/账号代际/会话/期限、只返回最小id/state/期限信息、版本错配拒绝，不返回短码/钥匙/密文；它是前台提示线索，最终授权仍经原PAKE与签名流程。Flutter前台单提示/去重接线未完成 |
| 最新公开Docker注册与持久化 | 仅公开server `bd86fec` 的独立archive实际Docker29.4验收11/11 PASS：默认关闭注册且无需邮件时首号注册/登录、同卷重启后永久首号标记、后续403且原账号仍可登录；UID1000、目录0700、network none/零公开端口、远程明文绑定拒绝。只有RAM随机合成登录派生值，容器/卷/镜像已删除并独立复核不存在，无源码改动；真实SMTP、TLS代理及手机连接未跑 |
| 独立App PIN密码学与限流 | core `64a1c4e03fd28b7f5c1981670e5b958dbfedd6a0` 八个冻结文件公开。固定公开core504+仅新包的race 11主/4子PASS、24.636秒，含真实子进程预扣后被终止再重开3.089秒；vet、gofmt、范围检查PASS。Argon2id64MiB/t3/p1、随机Vault钥AES-GCM封装、完整尝试锁/CAS/durable预扣和单用lease通过。本机Android能力分类/Keystore存储/升级latch与UI尚未验，不能称PIN产品可用 |
| Windows ARM64固定原生配对 | core `292e3f761b6a0c6d21793df70fcbbf6b7e977f8f` 十四冻结文件公开，BoringSSL与官方LLVM-mingw输入固定SHA；实际Windows ARM64配对11主/12子及上游SPAKE2六项PASS。CGO0直接Vault入口guard Windows实际login/pair两子项PASS。root在Mac只取public64a1+完全同SHA十四文件，Go1.26.4 CGO0 Windows CLI默认AMD64/ARM64构建分别11.716/11.697秒exit0；不等于nativeCLI入网。2GiB VM此前两次默认AMD64及一次CGo guard编译内存终止均保留失败；Windows race、正式owner/IPC/无人登录boot未跑 |
| Windows原生系统组件有限实验 | 历史profile/Batch/未记录COM数值失败均保留；修正LSA长度后profile HRESULT0与Batch NTSTATUS0。XML实际注册数值80020009/8004131a已独立观察，原hresultRecorded=false不追认修改；原生flags1旧XML FAIL、新XML PASS且零任务，strict resolver/readback/cleanup/absence均通过。其后fixed helper fresh单次注册FAIL，HRESULT80020009/SCODE80070005/WCode0，保持Pending；只读确认目标folder已给exact SID读写执行、无task/children/ownhelper，twoStopped。INPROC CoQueryProxyBlanket返回E_NOINTERFACE，不能推断目标token或COM上下文；进程COM设置仅待验证假设，未提升账号/ACL或换SYSTEM注册。正式服务未启动、CLI/provider/kernelboot未验 |
| Flutter产品化迭代与真实接线 | 四意图原生真实3/3见下；public mobile c185加逐SHA冻结16候选及独立gateway4切片，root离线依赖0.532秒、analyze2.629秒、68/68 PASS4.770秒。loginAccount/restoreSession/businessPendingInfo/retryBusinessOperation仅已知独立原生证据与明确opt-in开放，冷审批来源0继续拒绝、overallready=false；候选未公开。新空实例实际Flutter产品APK已构建通过9.410秒；testAPK首轮FAIL1.680秒，误编历史测试和缺CMake；仅filter本次两个驱动并复用Flutter参数后PASS1.413秒，历史源全部原SHA还原。该构建记录不包含安装或实际操作。视觉等待用户选择，不将静态样稿或编译计作产品链通过 |
| PIN成熟Go业务包装 | core `1544501fe6b1637d3cbe49d347c39a56ee344fe1` 三个精确文件公开，root逐SHA及全部源码审阅，仅public32加三文件独立race6主3子PASS，包11.732秒/进程13.969秒，vet0.783秒PASS。固定native package/slot/endpoint、MAC scope、完整intent和原ID、durable预扣/settle/release、私有one-use lease、成熟Workflow及同步保存门槛；logout保存失败如实返回local deletion标记，Recovery入口关闭。新的Kotlin五文件候选仅真实gobind Java ABI/Kotlin编译及5主机合同测试通过；固定public154/Androidarm64的独立candidate AAR构建PASS14.486秒；JNI/classifier/PIN批准CLI尚未验，正常7af AAR与Plugin/UI/cap未改，不称PIN产品可用 |
| Android账号四意图最终通过 | core `32b0293` 六文件和mobile `c754a59` 六文件按原SHA公开；固定原生source `a8848ce5`、AAR `7af22552`、APK `b0b51ecf`，3/3真实阶段29.498/17.077/15.795秒、合计62.370秒，22次CryptoObject和两次force-stop。注册/邮件证明/错误密码/登录仅authenticated且trusted=false、未可信restore拒绝、完整新码重输及精确account/generation/device视图通过；变量接受502后unknown/applied=false、环境create先验baseline1再delta1/total2、两次进程重开原ID续办计数不增加、退出删除本地alias/文件通过。root独立公开core292加同6文件，两个Go定向race PASS1.521秒（进程3.810秒）。所有测试PIN、包、forward和HTTPS服务清理；Flutter产品点击链仍未跑。此前三轮phase2 FAIL和preinstall未执行记录保留，不能追认其未知历史结果 |
| Android独立PIN存储实际验收 | mobile `f108a55` 七个冻结文件公开；root固定public56加原7文件，主机编译2.899秒、7项0.136秒PASS。独立无额外权限Android包真实Keystore MAC/AtomicFile/CAS/升级latch5/5 PASS0.156秒，am instrument0.421秒、安装清理总0.859秒；两个新包卸载且原172个包名逐字一致。实际能力classifier为BLOCKED，不能把存储通过宣称PIN可用；Go PIN wrapper已公开并通过独立测试，Android JNI及产品接线仍UNRUN/CLOSED，无系统PIN改动 |
| 独立debug产品TLS配置 | mobile `c18598a` 七文件公开，固定publicf108加明确6源码及中文证据；公共配置只允许唯一canonical loopback HTTPS地址和单公共CA、默认与release关闭、独立.productfixture包、不改变系统根。原生系统认证前校验固定endpoint、错地址清短码；无调用方CA或hostname绕过。host与debug/default构建及6项Gradle精确拒绝均PASS；root另逐SHA复制7文件，以同公共测试CA独立固定Kotlin/Java编译2.028秒、15边界运行0.100秒PASS。旧CA私钥/服务已清，下一真实链需新空实例/临时CA重新绑定APK；Android MethodChannel/TLS网络/Flutter产品链尚未跑 |
| Claude静态视觉版本与选择 | 第一版四页面HTML由本机Claude Opus5.5 medium实际生成，132.319秒、成功读取6张确切参考图；源码SHA `4adc2ebc8e96c863d4fe6b1fc8d33deca88582be0e6222d6c6135545a9e3512c`。root真实Chrome600×960/2x渲染四个390px手机页面、目视完整并存Library；430px画布外部预览工具栏溢出仍未修，不冒称窄屏通过。用户已看并要求整体换风格，旧稿未采纳；新版单次有限Claude调用116.485秒实际生成同内容A/B/C，成功读取四张确切旧截图与原HTML，不由Codex指定审美。源SHA 7159e5401d7dffed56e1cfe23e0e873e7b940c83e0cd1ae4aef2343a81bc5fb9，root实际Chrome同600×1000/2x渲染三页、目视完整并分别存Library，先A后B/C交付；仍待用户选择，不把静态样稿当Flutter成品或账号安全验收。后续单次Claude D/E调用239.446秒，实际Read原ABC源码及三图，提供各自明暗登录/主页共八屏及两总览；按用户要求再用单次C砂岩调用104.979秒，实际Read原C图和ABC源码，C源SHA c3df93f71379ed2792c806c4e8c23b3a11c5be25361755a2c676e8fdac1a829a。root真实Chrome单屏600×1000/2x与四屏900×1920/2x，PNG完整/CRC/目视检查后全部存Library并已交付。仍待选择，未更改Flutter视觉或产品门槛。 |
| Android恢复三阶段最终通过 | 已公开 core `5040921e1867578be291b985736811326ed8ae6d` 的14文件与mobile `56ac910d4c1778083d94fe2091a84392aa603362` 的8文件，逐blob按冻结22文件发布，保留后续live intent接线。实际测试为workspace14a27f9/core6eec463/mobile d169d74/server02689/protocolde21d99的固定基线+15编译覆层；最终source SHA256 `c7630a1971f3fe153543dc02f2a75dddaa2db697d109f1b8e0b2a8f1ce06fb7f`、AAR `54b4d03dc4fccaeec9cebf0655f6e15f14bc86a49c19d512865a9ccdfee8766a`。实际3/3 PASS，38.985/21.174/34.316秒，总94.475秒；30次CryptoObject含1取消、两次force-stop及三进程。覆盖受限恢复、完整新码中断后重新输入、明确X RO 1小时/Y Admin登记、最终seal故障后原ID恢复、X拒写/Y写、恢复手机高层V4批准正式CLI4及daemon真实Boot/Pull/IPC。两个控制器exit0，独立测试包/alias/state/PIN/forward/CLI/HTTPS全部清理；不等于新Flutter/PIN/四intent或第二次DAG恢复通过 |
| Android恢复第四轮失败历史 | 严格时钟修复后的source ef20/AAR abad4c95实际phase1/2通过，phase3测试夹具将规范角色值RO错误写成ReadOnly，整轮仍FAIL。只修夹具比较，原角色/保存/拒写门槛不变；最终fresh完整3阶段通过如上。前三轮失败继续保留，不将旧产物追认为通过 |
| 分层Flutter界面与截图 | mobile界面提交 `b6506e734822b94de43dd5a983fa9c1720cfe4b1`，32/32控制/导航/连接/并发退出测试PASS，analyze无问题。最终preview APK `cbe850d2a9915df51b6c99770ca55f0abf03533eb9fea47e941e032cb3e3ff0d` build4.4秒；默认APK `b20d5f5ed2ae6e4370062d7afb02c1f1d73ba7f1ced35727aefd6d307afff51c` build6.0秒；七个编译UI文件SHA与该公开提交匹配。复用AAR abad4c95，仅证明UI运行。实际安装/点击、默认连接页、显式演示入口、环境列表→详情→独立编辑、设备/设置/安全二级、退出及force-stop返回连接页均运行并截图，9张已存Library。只有默认入口为真实未登录状态，其余合成演示，不代表账号/信任/恢复状态已接产品；新gateway实际账号链未跑 |
| 注册策略与连接发现 | server `0454d8ae8697252d2e3ea65fbe134c2a44c5e763` 的26文件公开，245/245 PASS、0FAIL/SKIP/cancel、typecheck/build通过，31.92秒/测试31.56975秒；15项NodeTCP/真实workerd定向通过23.9851秒，本地Argon1474ms。仅两开关；closed且从未完成首号可注册、邮箱pending不独占、永久CAS标记，注册时固定verification要求。closed→open正确密码可补完已proof-ready输家，wrongpwd不行且旧赢家标记不变；reset不重开。GET /instance-info校验所需最小DTO已公开，不返账号数量。历史两处夹具失败及修复见子库，未削弱业务门槛；无线上迁移/邮件/部署 |
| 空实例合成注册启动器 | server `909d4988c8de7436a648623b2b852ec69bff41ad` 单独公开测试文件，SHA256 `ab6fdb3abd62f7f43d4c2f160bfc5e05ef517e30de01d1abc9d0e474a4c39cb1`。默认closed无需邮件首号与require-email/capture证明首号两实际HTTP场景PASS，确认起初零账号、登录后无预置设备/环境/恢复根；目录0700/库0600、SIGTERM清理PASS、typecheck通过，生产dist排除。Flutter HTTPS及测试CA链尚未跑，不能说产品注册已完成 |
| 前一公开固定源码完整39项 | root独立gitarchive提取 workspace `ce264405fdf477c53a8a417d99eb319235356edb`、core `6eec463eecc0e14f18dcb0ddd29ae2bcd98cad94`、server `ae65ff6ae6a5a34857c5fb081c63d62152daba09`、protocol `de21d9907dd7b73636afeabee4806655aaa0a82e`、mobile `d169d74d5c7bf080d02e950e6a8147d21175cc5b`。完整 `go test -race -tags harmonia_boringssl -count=1 -v ./acceptance` 39/39主项通过、0FAIL/SKIP、247.939秒、exit0；原17恢复负例、高层V4原journal/正式CLI、恢复后E/F控制与CRUD、原ID重试、严格挑战窗口/取消均实际执行。仅工具缓存环境与合成账号，无working-tree overlay，快照已删除。Android新恢复产物、Flutter重构、新PIN与首号策略不属于此公开快照，不把其进行中结果计入 |
| 前一公开core全部原生race | 上列完全相同五个固定提交，独立公开snapshot `go test -race -tags harmonia_boringssl -count=1 -v ./...` exit0，307主项PASS、0FAIL、1SKIP；12个含测试包全部通过。唯一skip为仅适用于默认构建的 `TestDefaultPairCannotCreateProtectedFilesOrSendRequests`；native SPAKE2实际执行。cmd7.362秒、cryptox41.691秒、mobileworkflow10.506秒、syncclient11.082秒。含新DAG与严格挑战修复；没有未提交Windowsservice/AndroidRecovery/PIN切片，不证明那些平台已运行通过；快照已删除 |
| 同一公开server独立完整检查 | root仅gitarchive公开 `ae65ff6ae6a5a34857c5fb081c63d62152daba09`，无工作树overlay，仅固定node_modules依赖与工具路径。typecheck1.779秒/build1.112秒exit0，全部230/230、0FAIL/SKIP/cancel，进程21.851秒/测试21.715秒exit0；实际本地workerd Argon同参数首登录2038ms，不是线上配额。临时源码删除，日志私有。本结果不包含正在实现的新注册策略/instance-info |
| 连续恢复及CLI4完整公开快照 | root独立提取公开 workspace `2ba0c1a7dc8754165be8c365714d2fce2d164bd9`、core `457858f879a264675c0e8598e06529bd87d6344b`、server `b968223d85872cb70536ba4ffaf0b415f65971cd`、protocol `b7700013c378fb59afc7d401fc1a0de05e45e458`、mobile `d169d74d5c7bf080d02e950e6a8147d21175cc5b`，完整 `go test -race -tags harmonia_boringssl -count=1 -v ./acceptance` 33/33主项通过、0失败/跳过，161.661秒、exit0。包括原27项、新连续恢复五主项12场景、正式恢复E→CLI4真实SPAKE2/HPKE、原receipt丢回应恢复、CGO0 daemon及RO拒写/RW写删。只使用工具/缓存环境及合成账号、TLS和钥匙；无工作树覆盖，临时快照删除。此结果不包含尚未公开的高层V4审批、恢复E/F控制或Android恢复/UI |
| 同一公开源码完整Go原生回归 | 上列同一五个固定公开提交独立快照中 `go test -race -tags harmonia_boringssl -count=1 -v ./...` exit0；293主项PASS、0FAIL、1SKIP，12个含测试包全部PASS。跳过项精确为 `TestDefaultPairCannotCreateProtectedFilesOrSendRequests`，仅适用于默认构建的反向检查；真实原生SPAKE2执行。cmd8.358秒、cryptox8.911秒、mobileworkflow4.880秒、platform1.967秒、syncclient9.217秒。Windowsprofile仍只含合成生命周期与编译证据，未冒称Windows原生API或无人登录通过；临时快照删除 |
| 新服务公开快照兼容回归 | root独立提取公开 workspace `67a69af08823bf14c305dd6aefcd795e57cd0bb3`、core `894f2ad8c9d05fd44b7b97533921b15ee94c76d2`、server `b968223d85872cb70536ba4ffaf0b415f65971cd`、protocol `b7700013c378fb59afc7d401fc1a0de05e45e458`、mobile `3b72a29423527794c6969c6b0f9243a980ad1ddb`，无working-tree覆盖；`go test -race -tags harmonia_boringssl -count=1 -v ./acceptance` 完整27/27主项通过，120.670秒、exit0。实际原生PAKE/HPKE/AEAD、正式CLI/daemon、暂停四场景、管理五故障、纯时钟回退、原17恢复安全场景均回归；这证明新服务与既有公开客户端兼容，不把尚未公开的新连续恢复客户端算入结果。仅使用工具/缓存环境与合成数据，独立快照已删除 |
| 新公开用户流程独立定向复验 | root仅提取公开 workspace `f1d10b1d313a90ce82c2e3627019d4d3a7d8d952`、core `202a83ca155bb9dacd51f66262ea01c95ebb2934`、server `02689b44e071cfdfe0aa3c65d7a5d250c3c69fe8`、protocol `2edad19729ec0156bda89756a3e29c72d1252844`、mobile `d169d74d5c7bf080d02e950e6a8147d21175cc5b`，原生race5/5通过69.442秒、0FAIL/SKIP、exit0。实际高层V4六保存场景32.19秒、新高层正式CLI4与最终seal9.68秒、原CLI4 RO/RW9.09秒、E/F控制15.40秒、跨New CRUD原ID恢复1.60秒。只有公开源码，无overlay，日志保存在本机私有临时目录、快照删除；该结果不包含后续高层环境新文件、Android恢复或UI |
| App重启的CRUD原ID重试 | core `202a83ca155bb9dacd51f66262ea01c95ebb2934` 已公开：受保护待处理列表只六字段元数据；Writer仅原ID/Operation=retry、环境仅原完整签包，不接替代值/密文。独立公开workspace14a/core1be/server02689/proto2edad仅3精确候选，变量及create各accepted502→真正Close/New→原ID→同状态/Pull确认，各1POST，已Applied原ID可再确认，未记载ID拒绝不建事务。1主项race3.062秒、最终mobileworkflow包race3.027秒、syncclient8.811秒、vet/扫描通过。原生Android新入口和真实App kill尚待验证；本结果不等于UI通过 |
| 受保护高层V4审批 | core `1be168018ed7bafd052422604dfe2ed3b78db3d2` 已公开。真实HTTPS/SQLite/nativePAKE六保存与unknown场景通过34.273秒；另高层E→正式编译CLI4/504原receipt恢复→CGO0daemon新Boot/Pull/RO拒写，加管理者最后seal失败原id恢复通过8.90秒。原CLI4 RO/RW回归9.34秒，三个主项及六场景联合race52.493秒。包native-race4.648秒、vet/源码检查通过。原17负例未改；helper仅增加明确可选审批回调，原默认段及断言不变。Android尚待新独立验收 |
| 恢复E/F真实Go管理控制 | core `de7bb21ebe80a25d9204d40c5e242bc476cbbbc9` 已公开。public workspace14a27f9/core1be1680/server02689/protocolb770001基线仅叠8冻结候选，逐文件manifestSHA `5674871b9d2e376569d7f75aaa32a69563f2ab4ad2e48fe13f33bd7dcff1503a`；真实E创建/504原hash确认/PAKE F/HPKE读写/全部receiver与livevalues轮换/F新KV写/RO拒写/none清缓存/global撤销后freshBoot拒绝，native race16.18秒、Go17.562秒通过。显式historical=false/default仍执行当前checkpoint门槛，单true才验证历史；必要DTO race3.685秒通过，包race/vet/三OS编译通过。此候选文件已原样公开，root纯公开定向复验已通过；手机高层P3环境journal现随006450d公开，原生接线继续 |
| 恢复后手机高层环境原请求 | core `006450dfb1a338b20c182c5cf5c16debf4ff102b` 已公开，4个Go文件及独立新验收逐SHA冻结，候选manifest `9c3fcaacd9f67ca4dfd971ed8e32b41cfd3930043cb1ca1e399ed932cfc281c6`。固定公开workspace14a/corede7/server02689/protocolb770仅叠此切片，实际高层E创建504后原journal New恢复且只1次接受POST、变量写、轮换最终保存拒绝后原包New恢复、重命名保值及删变量/环境通过；普通3.42秒、native race12.04秒（Go13.419秒），包race3.058秒、vet、三平台CGO0测试编译通过。原P2日志/profile不自动升级，原根pin不变，当前恢复recipient仅来自完整已验证账本；手机原生桥、Android/UI未跑，root纯公开新target复验另记录 |
| 严格恢复挑战时钟等待 | core `6eec463eecc0e14f18dcb0ddd29ae2bcd98cad94` 三文件已公开。Android实际设备比服务器慢约0.35秒时，原expiry+120在本机Unix秒差成为121，签名先拒且登记POST为0；没有改设备时钟。修复仅在完整typed绑定验证后等待真实本机时间进入原120秒窗口，固定monotonic总预算5秒；每轮重查原owner期限、回退、取消和native保存，失败清RAM owner，原expiry/nonce/ID/签包/密码学门槛不变。独立公开workspace6210/core006450d/server02689/proto2edad加本四文件候选：真实HTTPS两场景race通过18.815秒，unit及旧session/clock race通过7.114秒，旧密码学2主30子通过1.991秒，vet/扫描通过。最初保存失败unit错误期待raw callback已按既有sanitize合同纠正，业务失败/清owner断言保持；原17安全场景文件未改。新原生Android三阶段尚未复跑完成，不能报手机恢复已通过 |
| 重复恢复平坦图密码学内核 | protocol `de21d9907dd7b73636afeabee4806655aaa0a82e`、core `bf8ed9a8c1f707e376405134fbcb77612793dd31`、server `ae65ff6ae6a5a34857c5fb081c63d62152daba09` 已公开。Go独立公开core006450d+13精确候选cryptox race62主242子通过41.997秒，新增9主39子通过2.791秒，vet/三OS CLI编译通过；protocol25/25，root再实跑25/25通过166.045ms。真实合成原初始化→恢复E→E新Z→再次恢复G三环境Admin→G全Admin继续轮换→H三环境RO，Z实际HPKE/AEAD解密；原根不变、图无嵌套、caller输入和返回数据隔离。服务端新47项及全230/230、typecheck/build通过17.03秒（测试16.6576秒）。详细逐文件证据在子仓中文docs；新major2/HTTP/实际CLI与手机第二次恢复均未接入，库通过不等于产品闭环 |
| Windows ARM64成熟SPAKE2可行性 | 仅隔离Ubuntu新tmp构建固定BoringSSL fab96f与SHA已核对的官方LLVM-mingw20260922，Windows官方UTM执行上游SPAKE2六项实际6/6、exit0/414ms；独立Go1.26.4 CGo探针同码8轮、错码8轮及损坏消息拒绝实际通过exit0。首轮链接缺静态C++/线程runtime失败留档，使用同固定链static libc++/libc++abi/winpthread后通过，PE为ARM64且无第三方runtime DLL。该实验未改正式源码/tag/系统PATH、未安装服务、未生成可信fixture；Windows原生pairing接线和正式CLI/SCM/boot仍待验 |
| 恢复后高层环境纯公开独立复验 | root独立gitarchive仅提取公开workspace `9eb77dffa228e9103b49a3865e03cea144a8949e`、core `006450dfb1a338b20c182c5cf5c16debf4ff102b`、server `02689b44e071cfdfe0aa3c65d7a5d250c3c69fe8`、protocol `2edad19729ec0156bda89756a3e29c72d1252844`、mobile `d169d74d5c7bf080d02e950e6a8147d21175cc5b`，新 `TestRecoveredMobileEnvironmentCRUDOriginalJournalAndFinalSave` 原生race1/1通过13.497秒、0FAIL/SKIP、exit0。无工作树overlay、仅合成材料与工具cache，临时快照删除；这不是完整38项或Android/UI验收 |
| Android恢复首轮未通过 | fixed公开workspace14a27f9/core1be1680/mobile d169d74/server02689/protocolb770001加15有限原生候选；source85ec6317/AARa04c6c3f/CLI46385770，bridge与registry两套race、正常AAR/Kotlin/testAPK构建通过。真实AVD phase1停止于19.446秒、8次认证含1取消：受限View/认证取消关闭owner/显式resume均执行；故意保存失败返回固定平台GO_OR_KEYSTORE_REJECTED，夹具错误期待可decode业务fault。本轮FAIL，phase2/3未跑；只修夹具错误分类并保留0POST/owner关闭断言后另fresh验收。PIN/两测试包/文件/forward/HTTPS已清理，原AVD/preview保留；不把此前管理42次认证算恢复通过 |
| Android恢复第二轮部分执行 | 同一公开base与15有限overlay的新 source `d4f6bf80`/AAR `3f528fd0`，仅修首轮夹具错误分类，实际phase1通过33.462秒并force-stop。phase2新码中断恢复、原状态/完整恢复视图及普通管理仍拒绝通过，显式登记处11.216秒停止：按第5次Save注入故障被额外时钟保存影响，返回REJECTED而非预期最终保存PENDING，phase3未跑。成功登记计数0仅数200，不证明0POST；改夹具为真正最终checkpoint保存谓词，保留accepted-not-applied/owner关闭/原ID恢复门槛后另fresh跑，不放宽产品安全门槛 |
| Android恢复第三轮真实拒绝定位 | 保留新的fixed候选source `22793f47`/AAR `17087d59`：phase1通过34.359秒；phase2失败13.693秒，阶段诊断saves=2、challenge status=200、实际登记POST尝试=0。因此这一轮并非最终保存故障，失败在挑战后、原登记packet密封前，正在定位真实Go/crypto拒绝；phase3未跑。不将第二轮假设当结论，不放宽来源/登记门槛 |
| 恢复E/F环境与权限管理服务第1层 | server `02689b44e071cfdfe0aa3c65d7a5d250c3c69fe8` 已公开，183/183、0失败/跳过/取消，typecheck/build通过，总19.52秒、测试19.07秒。Node/workerd定向5/5通过8.719秒；显式Proof3控制及环境v3原两签提交、E新建Z、未获Z授权F的完整身份归档、RW/Admin、F新环境和轮换、rename/delete、none历史来源、原摘要/事务尾和SQL原子回滚。封套和值是合成结构输入，真实Go HPKE/PAKE及Android高层新链仍待独立验收；未把目录公钥当信任，不加独立恢复vault列表，旧接口/parser/domain保持；下一次恢复DAG仍独立待办 |
| 连续恢复服务切片 | server `b968223d85872cb70536ba4ffaf0b415f65971cd` 已公开，178/178 通过、0 失败/跳过，typecheck/build 通过，总18.25秒、测试17.88秒。18项新增向量/Node TCP/workerd HTTP覆盖旧设备全部撤销后的25域双签过渡、仍受限、明确新设备18域双签登记、boot/proof3/v4委派、当前降权重查、SQL整笔回滚、原ID/hash查询和JSON/容量边界。HTTP封套是合成结构输入，此服务测试不等同真实PAKE/HPKE、手机强认证或新码回填验收。内嵌recovered actor proof3尚未接，后续复轮/部分新增环境再恢复失败关闭；未跑本批Docker/Wrangler，未部署/发布/加入CI |
| server `mise run check` | 最新公开完整恢复来源图 `e4b3cf5` 144/144 通过、0 失败/跳过，类型检查和构建通过，任务 15.68 秒/测试 15.294 秒；独立 Node/workerd 恢复图 10/10 通过，7.536 秒，本地 Argon2id 相同参数约 1475 ms。此前原初始化锚 134/134、19.27/18.8906 秒保留；完整 before/after 授权与身份、历史写入者来源、精确原 genesis、缺原记录拒绝、跨环境数据隔离和 SQLite 原子回滚均回归。此前 v2 109/109 记录保留在服务端文档；本次未重跑 Docker/Wrangler，也未部署 |
| SMTP TLS | 隔离证书的真实 TLS 握手 2/2 通过：强制 TLS、证书验证和拒绝降级；没有真实邮件投递 |
| Workers / Docker | 本地 workerd D1 仅邮箱目录、账号 SQLite DO 与 64 MiB/3 次/p1 Argon2 路径通过，首登录约 1387 ms；当前 v2 Wrangler dry-run 265.14 KiB/gzip 65.50 KiB 通过，无部署。2026-10-02 UTC 最新 v2 Docker 镜像重新构建及隔离持久卷重启 smoke 通过，验证合成空账号持久化、非 root、0700 和拒绝远程明文绑定；本次容器/卷已清理，未发布端口 |
| workerd 关闭边界 | 到期 alarm 的真实 4003 关闭帧通过；Miniflare 代理 TCP FIN 延迟，曾导致标准 close 事件五秒超时。没有宣称 TCP FIN 或线上休眠/容量通过 |
| Go 回归 | 首根审批 7 主项/27 子测及 native race 2.157 秒通过，十包普通 race 与三平台编译通过；统一手机错误码白名单后 mobileworkflow/syncclient/mobilebridge race 1.834/2.499/1.486 秒通过。SelfRevoke 两包 race 和真实 HTTPS 联合 race 通过。IPC 最新本包 race 3.051 秒、vet 和三平台测试编译通过；新增容量回归曾失败，实测 ENOTCONN 漏分类后修正，最终通过 |
| 最新 Go 来源回归 | 公开 `0787b9f` 本批冻结源码的默认全包 race、vet、三平台 CGO=0 构建通过；root 另以仅工具/缓存环境完整运行 `go test -race -tags harmonia_boringssl -count=1 ./...`，全部包通过，真实原生 SPAKE2 不跳过。cmd 7.806 秒、cryptox 2.268 秒、mobileworkflow 4.048 秒、syncclient 3.932 秒；协议 17/17、三份环境来源向量 SHA256 相同。手机高层真实 HTTPS 定向 race 3 主项/7 场景通过 15.396 秒，包括遗漏一条轮换内写入时数据检查点不前移、原 ID 全量恢复才标记已应用。该 Go 测试不能代替 Android 新构建验收 |
| 独立 Ubuntu init 重启 | 新隔离 Ubuntu24.04 ARM64 内原生 SPAKE2 6/6、firstroot/真实入网/签名共享写后，删除登录 slot/引导输入并清旧服务端 session；只重启新来宾后 UID30001 无登录，新 boot/pull 200、无密码登录，正式 IPC/sh/第二 UID 拒绝、cap0、0700/0600 通过；本次账号/units/keys/SQLite 已清理，新机正常停机保留 |
| Linux 开机边界 | OrbStack 为 LXC，namespace boot ID 变化而内核 uptime 连续，因此只证明 init 重启。全局 LXC drop-in 关闭部分 systemd sandbox，未修改；物理内核开机、磁盘解锁和完整 VM sandbox 未跑 |
| UTM Windows/macOS | 两机正常启动；历史Windows guestexec -10004，当前官方exec已精确输出合成标记且文件往返通过，SYSTEM/Session0只读验证成功，工具缺口解除。macOS exec后端不支持，执行器无CUA且Accessibility false；未改安全/登录设置。正式Windows SYSTEM broker/S4U用户token/受控profile服务正实现，实际服务、hive及物理boot未跑，正式daemon仍关闭 |
| Android 实际构建与 UI | 官方 NDK r28c SHA1 实际核对，Flutter APK 构建、隔离 API34 ARM64 AVD 安装启动通过；7 张真实合成预览截图已交付。首次 Maven TLS 短暂失败，自动重试通过；不使用宿主真实值 |
| Android 窄原生桥 | 此前实际 AVD 窄桥 6/6 通过；最新同一 AAR/main/test APK 的完整原生 12/12 通过，184.013 秒、40 次设备密码提示含取消。覆盖真实注册/邮件证明/完整新码重输初始化、boot/pull、环境/变量 CRUD、原 ID 已接受 502 恢复、BUSY/保存失败/TLS 负例、known200 与 accepted502→status401/boot403 的自撤销；未知结果不虚报 completed，两个分支均清 alias/key/state。PIN、独立资料/包已清，预览与 AVD 保留；没有打开 Flutter 默认网关 |
| Flutter / iOS | 静态分析与 9/9 控制层测试通过，不含 UI 单元测试；已移除生成的 UI 测试目标。iOS project/scheme 解析通过，iOS 未构建验收 |
| 多管理设备证明 | Go 密码学 7 项/29 子测、Node 协议 14/14、v2 来源范围 6 项/21 子测及原生配对回归通过。真实 A→B Admin→v2 C RO，经 SPAKE2、B 本地根 pin 与逐环境证明、HPKE、已接受 504 原回执恢复和独立 daemon，验证 A/B 历史、RO 造密文拒写、B 降权立即拒写及 C 撤销清除。来源不依赖服务器目录或全局 Managers；该 v2 历史基线不包含新环境；新 cert3 来源纵链已实际通过，见新来源整合行 |
| 当前整合检查 | 加入 WS 依赖后顶层 go.sum 曾缺失而 setup 失败，已同步并通过后续 7/7 及最新独立快照 8/8。Go WSS→真正 Node/SQLite 的单次票据、断线持久序号补漏、暂停仅授权、恢复及 4003 后重查通过；通知不推进数据检查点 |
| 公开范围 | 基础源码秘密/个人路径/编译产物扫描与人工范围检查通过；只使用合成账号、临时 TLS 和独立 provider，无宿主真实 env/凭据。这不是完整秘密或生产安全审计 |

IPC 的受控复现使用真实 FileStore、原 1 秒预算/32 容量：旧实现排队 1119ms 后仍执行 override，响应头超时被误报协议错误。修复后请求约 1001ms 离开队列且未执行修改，已开始的 Save/fsync 仍完成并准确记录回复超时。原 20 并发全部成功断言保留，定向 race 三次通过，最大排队 278–286ms。此前三次失败没有原阶段记录，无法追认确切历史时序；超预算时仍可真实失败，不能把无回复当未执行。诊断默认关闭，只含固定类别与耗时。

首次管理设备从独立 Ed25519/X25519、用途分离恢复钥和独立环境 HPKE 封套开始；初始化挑战绑定账号/代际/登录会话，设备与恢复钥双签后一次事务接受。配对使用固定 BoringSSL Edwards25519 SPAKE2 draft02 profile；短码只在端点使用。未链接成熟原生库默认拒绝，不声称 RFC9382 标准向量通过。

暂停独立拉取授权投影，执行撤销、到期和删除墓碑；恢复按原数据检查点补漏。创建/轮换须附齐当前设备和恢复封套，旧版本写入立即拒绝。最后环境删除须明确账号管理权限，当前关闭。退出先持久 AccountClosed/epoch 再清材料，崩溃重启不得复活旧设备；逐 key 恢复原值，保留无关修改。

仍需完成手机批准/恢复/轮换的完整产品闭环、恢复连续授权及恢复后显式管理登记、既有设备管理的手机接线、三平台物理无人登录开机及 iOS。通知客户端重连补漏已通过上述真实联合验收，Windows 服务与通知联合实机仍未跑。Worker 线上 Argon2 资源、容量/分页和完整安全审计未完成。先按可测试 M2 切片继续，再实现已授权自动更新与 CI；正式 Tag/Release/安装包、签名钥生成上传及真实线上部署仍未授权。当前不能宣传生产可用。

## 正在关闭的安全门槛（不计为完成）

仅固定根设备公钥不能限制伪初始化授权。公开来源验证器现固定受保护原双签入网回执或初始化中精确的原初始授权集合，控制证据候选不能新增 genesis；真实非根新环境/轮换及已密封重启的 Go 联合主项通过。公开 `220cf5f` 已将缓存数据来源和当前授权分别验证，暂停纯 KV 轮换保留旧配置；缺连续来源仍拒绝，撤销/删除/到期立即清理，真实原生四项安全回归通过。

公开 core-go `1801392` 已包含受限恢复切片：使用当前完整恢复码认证的精确原根，从已接受原初始化的完整 proposal、proof、设备与原恢复两签验证初始化承诺。当前恢复公钥可以因轮换改变，原初始化记录不改变，只有原 proposal 精确承诺的初始授权成为 genesis；缺记录、nonce/签名篡改、重新计算 proposal hash、根设备真实签一个新 Y 授权后伪装初始化均拒绝。此业务代码针对当时服务端候选的真实 HTTPS/SQLite race 验收实际 5 主项/17 子项通过（18.626 秒）。现在对应服务端已公开 `0d4d08e`，相同四子仓公开固定源码的独立恢复 race 18.628 秒和完整 13/13 race 30.845 秒均通过，含新增/轮换后的 fresh-context 来源缺失负例。对应组件 race 及隔离 core-go `731984a` 加仅恢复六文件的独立包 race 也通过。

旧公开兼容入口只恢复已验证的初始环境；新增 explicit-origin 候选已通过非根创建、跨 KV、所有旧设备撤销及完整封套承诺，原 17 条恢复文件逐字未改，本轮完整固定快照已通过。恢复码轮换接受后仍受限，不自动信任新设备；可验证恢复授权连续链及显式新管理手机登记尚未实现，Android新锚恢复后续已完成三阶段，见当前证据表；Flutter及第二次DAG恢复仍未完成。

Android 首根→CLI v2 审批在此前编译基线（core-go `c4dec971`，working tree modified）集中 1/1 通过 59.074 秒，mise 入口复测 58.733 秒；四个 CLI 控制器成功，覆盖 unknown 原 ID 查询及两次保存失败无批准 POST。当前 origin-aware CLI 显式 certificate-version 2 重跑 55.248 秒和诊断 54.702 秒均失败：known 分支通过，第二候选完成双签入网但 Pull 拒绝，daemon/IPC 存活，公开状态为 0/0/0。服务端完整控制证据闭包已补并公开，Go 校验不放宽。随后唯一 fresh focused 使用新正常 AAR 和显式 v2 CLI（`1801392+dirty` 构建标记，非公开固定源码产物），实际 1/1 通过 59.634 秒，21 次系统认证含 1 次取消，四个控制器退出 0；known、丢响应原 ID 确认、两同步保存失败无批准 POST、取消/BUSY、Logout 全部执行。CLI/provider、PIN、两测试包/槽/文件、forward、HTTPS/SQLite 已清理，原 preview 与 AVD 保留。后续源码变更没有冒称已由该产物测试，后续独立来源快照的 Android v3 已实际验收并公开，见新增行；实际Android恢复的后续三阶段证据见当前表，不扩大为Flutter已完成。此前 12/12 的范围仍不扩大为远程批准或恢复通过。

本轮Mac执行器只读确认UTM两机started；Windows正常官方guestexec和文件传输已验证，当前没有工具阻塞。macOS guestexec明确后端不支持，执行器无CUA且Accessibility只读false；最小剩余入口为正常来宾终端/SSH或正常桌面控制授权，未读取真实密码、未改虚拟机安全配置。父线程中途消息工具在能力刷新后不可用，本机与 GitHub 推送正常；本文件保存可复查结果，不以其它外部通知代替线程回报。
