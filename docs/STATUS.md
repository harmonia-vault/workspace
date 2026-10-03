# 当前实现状态与验证

更新时间：2026-10-03 14:00 UTC。M2 尚未闭环，项目仍是实验性安全软件。组件通过、实际产品步骤通过、整轮测试结果分别记录，不合并宣称完整产品或生产安全验收。全部用户功能及下一步见 [TODO.md](TODO.md)，完成条件见 [PLAN.md](PLAN.md)。

此前 STATUS（最近更新为 11:32 UTC）已[按原字节完整归档](history/STATUS-20261003-1132.md)，SHA-256 为 `2ab49f34deed0026d54a8efcf65a23442208eeb61207aaa665309d0591012079`。归档中的“当前”、路径与未跑结论只对应当时上下文；原始失败和精确历史快照没有删除。

## 当前范围

本次整理依据已公开 protocol `ccf8ec6`、core-go `6d8178a`、server `6c39ed1`、mobile `1cb8398`。运行结论仍分别绑定下列证据中的实际源码与产物，不能追认为这些最新提交的完整重跑。并行候选仅在明确标为候选的项目中列出，不算已公开能力。

| 用户流程 / 平台 | 已实际验证 | 仍缺什么 / 结果限制 |
| --- | --- | --- |
| 注册、邮箱证明与首号策略 | Node/workerd 永久首号 CAS 已验；Android 最终 C 产品完成连接、注册、邮件证明、普通登录未可信、完整新码首机初始化与 CRUD；iOS 注册/邮件证明及后续登录/初始化/变量 CRUD 分别有固定产物证据 | 真实外部邮件投递未跑；账号登录始终不等于设备可信。邮箱重置 App 入口仍待接 |
| Android 产品 → 正式 CLI3 | 最新真实产品轮完成账号→首机→变量/环境 CRUD→明确授权→原生 SPAKE2→正式 CLI3 入网、Boot/Pull、CLI 写入→App 显示；34 次实际系统认证、批准接受 1、mutation 5、Pull 74 | **整轮 FAIL 149.966 秒**：最后 logout 的额外系统认证窗口未找到。主业务通过与尾部退出失败分开记；不能称整轮 PASS或完整生命周期通过。[本轮证据](../mobile/docs/ANDROID_PRODUCT_USERFLOW_20261003.md)已公开于 `b0778e3` |
| Android 独立正式退出 | 同生产来源的新空实例最小回归 PASS，53.267 秒、9 次实际系统认证；正式退出后设备/workflow/alias 不存在，本地恢复被拒绝，正常 Activity 重启仍无材料；[证据](../mobile/docs/ANDROID_PRODUCT_LOGOUT_20261003.md)公开于 `cc22862` | 没有重复 CRUD/CLI；此前完整轮末尾失败原因仍未确定，不追认整轮通过；前台稳定只作测试排序，不能代替认证或产品忙状态证明 |
| iOS 产品 | 固定 mobile `8a0515c` Debug 实际登录未可信；显式新首机完整重输及提交 6.464 秒；变量新增/读取/更新/删除、正常退出和本地恢复拒绝通过；init 1、mutation 3/3、Boot 8、Pull 20；[证据](../mobile/docs/IOS_PRODUCT_INITIALIZATION_CRUD_SIMULATOR.md)公开于 `91e499c` | 第一次过期流程的完成/查询均 FAIL并保留；原查询 HTTP 次数未采集，不能称已接受未知结果不可查。仅 Simulator；真机因素、iOS PIN、审批/恢复和完整生命周期未验 |
| App 锁与 PIN | Android MainActivity原生三阶段PASS；Flutter最小输入链PASS40.683秒：真实双录、独立PIN restore、错误PIN零写、正确PIN一次写且同Pull验值、UI忘记和新身份NOT_TRUSTED；[证据](../mobile/docs/PIN_FLUTTER_RUNTIME.md)随16项逐操作门控公开于 `1cb8398` | 前五FAIL保留，精确ErrPIN分类已修。账号注册/邮件/首机初始化采用同设备真实原生bootstrap，不算PIN逐屏账号/恢复码向导；CLI批准UI、恢复、迁移、V4/V5及iOS PIN未验。系统取消/临时锁定不得降级；整体realVaultReady=false |
| 首次恢复及恢复后管理手机 | 固定 Android 原生三阶段 3/3、94.475 秒、30 次系统认证、两次 force-stop，通过受限恢复→完整新码→显式 cert4 登记→CLI4；[证据](../core-go/mobilebridge/RECOVERY_NATIVE.md) | Flutter 恢复向导与普通管理产品入口仍待接线和实际验收；不能借原生通过开放所有操作 |
| 重复恢复 DAG → CLI5 | 已公开 major2/证书5/P4 客户端与服务；独立真实 A→B→C 恢复、原生 SPAKE2、正式 CLI5 RO/RW、CGO0 daemon、Boot/Pull/IPC/隔离 shell 通过。主 race 32.89 秒、Go 总计 34.356 秒；Node/workerd 8 项通过；[证据](evidence/RECOVERY-DAG-HTTP-VALIDATION.md) | B/C 是 Go API与测试加密存储适配器，不是手机 UI。手机 DAG S1 journal/CAS/owner合同已公开于 `08dcf01`，根独立442项race与vet通过；仍无真实平台CAS/ABI入口。S2a原ID冷查询、P4环境CRUD实现中；跨操作owner、授权管理、manager-reanchor和新Windows SCM仍缺 |
| 设备管理、前台授权提示 | Go/TS 管理与 Android 原生 42 次认证有固定证据；最小 pending 元数据已公开；CLI3 手动批准产品主链已实际通过 | Flutter 设备权限管理、真实前台请求去重/单提示/badge、取消/到期/撤销清理仍需闭环；不增加后台推送或伪造事件 |
| Linux 无人登录后台 | 固定 core `b094a93`/server `bd86fec`：完整内核重启、新 boot/两 unit 身份、目标用户未登录时持钥 Boot/Pull；CLI 写入与交互 Bash 刷新片段纠正/暂停/签撤销/逐 key 回退通过；[证据](evidence/LINUX-KERNEL-REPRODUCTION.md) | 限本次隔离 Ubuntu；新版POSIX整目录消失恢复已独立通过48项shell/provider测试；共享离线退出和只读入网检查已公开；Linux安装器已有有限规划/日志/停止核查候选，总协调器仍在实现，其它发行版和三OS整体未验。已有进程env只能经正式shell接入更新 |
| macOS 无人登录后台 | 同固定 core/server 的正式配对、LaunchDaemon、清会话后内核重启未登录 Boot/Pull、重启后 CLI写入下发、暂停签撤销和精确清理通过；[证据](evidence/MACOS-LAUNCHDAEMON-VALIDATION.md) | 原观察器字面 `loginwindow` 检查 FAIL保留；独立页面/UID/boot/HTTP证据支持未登录结论。有限安装器v4候选71项race PASS含PID退出等待；源权限类bug负例修前FAIL保留。[新版POSIX终端恢复](../core-go/platform/POSIX-CLEANUP.md)已公开，根在06db加该切片独立race48/48、vet通过；[正式离线退出](../core-go/docs/OFFLINE-LOCAL-LOGOUT.md)已公开，根独立三包race163/163与vet通过；共享[只读入网检查](../core-go/docs/LOCAL-ENROLLMENT-CHECK.md)已公开，根独立218项race与vet通过；已停服卸载、中断重试和真实安装器VM仍未完成 |
| Windows 服务与用户环境 | 两 SCM服务可创建但从未启动，本轮精确资源清理及独立absence已通过；目标普通 Batch身份、同会话 SID、profile 加载/释放及权限恢复已有局部实证；最新 AccessCheck 80 次 API检查完成，目标自己的任务目录 create允许；同一隔离Windows进程内typed VARIANT调用帧9/9通过 | `ITaskFolder::RegisterTask` 仍 FAIL：HRESULT `80020009` / SCODE `80070005`。profile、AccessCheck与合成调用帧通过均未证明注册根因；password仅NULL→EMPTY的窄候选正在准备，真实新注册未跑；不扩大权限。正式 provider/CLI/SCM启动/无人登录 Boot尚未验 |
| Docker、Workers、邮件 | Docker固定公开 `bd86fec` 独立 11/11；Node/workerd账号与同 Argon2id参数有本地实证；新增 DAG服务端8项另列；隔离 SMTP严格TLS 2/2 | Docker真实代理/手机完整链、Workers线上资源配额、真实SMTP投递未跑；CF Email Service可选接入未开始；不降低安全参数，不部署真实服务 |

POSIX根独立验证的源码/日志摘要与边界见[记录](evidence/posix-cleanup-result.json)；离线退出16文件的根复验见[记录](evidence/offline-local-logout-root-result.json)。两者均未包含安装器VM。新增[移动DAG S1根复验](evidence/mobile-dag-s1-root-result.json)和[只读入网检查根复验](evidence/local-enrollment-check-root-result.json)均为明确限定的组件验证。Android最新产品轮已有公开精确来源/产物及清理证据；Windows AccessCheck和macOS安装器仍按本轮协调复核的候选范围记录，不计作公开树整体通过。各平台测试只使用合成账号、独立资料与局部测试 CA；没有导入宿主真实 env或凭据。

## 最新验证如何使用

- 最新完整旧基线为 workspace `8b84acb` / core `5040921` / mobile `56ac910` / server `909d498` / protocol `de21d99`：原生 race 39/39，251.752 秒；同 server 独立 245/245及类型/构建通过。它不含后续 PIN、Windows、pending路由、当前 Flutter产品链或 DAG新切片；[原始范围保留在归档](history/STATUS-20261003-1132.md)。
- 后续 DAG独立联合是一个主项/两个子场景，加服务端8项与受影响包检查；iOS是固定8a产品实际运行；Android与PIN各自有独立包/脚本。不能把这些局部结果拼成最新五仓全套通过。
- iOS已正常 logout、恢复拒绝、停止自己的夹具和RAM助手；Linux/macOS证据分别记录真实清理。Android原整轮logout失败与夹具清理分别记录；随后独立正式退出/材料清除/正常Activity重启已通过，仍不追认旧轮结果。

## 当前 M2 优先差距

1. Android独立正式退出已通过；Flutter PIN最小输入链已通过，继续账号逐屏/批准入口与剩余生命周期；明确取消、后台、App kill、切服务和失权后的状态与原 ID恢复。整体 `realVaultReady` 和未验能力继续关闭。
2. 接通手机恢复向导、重复恢复 DAG高层、P4环境/授权管理和manager-reanchor；保留完整新码重输、先受限、显式环境/角色/期限和最终下发/保存门槛。
3. 完成设备管理/前台授权提示和邮箱重置产品入口。Windows只沿实际最小拒绝原因继续；Mac/Linux正式安装器及各自 shell/多用户验收不能借单次脚本通过替代。
4. 按最终公开提交再作有界固定快照集成验证，保留历史失败。真机、真实邮件、线上Workers配额和安全审计仍是各自明确未验项，不无限阻挡其它已授权本机实现。

M2完成后再实现已授权自动更新和CI/CD。尚未配置或触发CI/CD；正式 Tag、Release、安装包发布和真实线上部署仍须另行授权；发布签名私钥生成/上传须批准或用户安全输入，不读取旧私钥。
