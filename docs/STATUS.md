# 当前实现状态与验证

更新时间：2026-10-03 23:00 UTC。M2 尚未闭环，项目仍是实验性安全软件。组件通过、实际产品步骤通过、整轮测试结果分别记录，不合并宣称完整产品或生产安全验收。全部用户功能及下一步见 [TODO.md](TODO.md)，完成条件见 [PLAN.md](PLAN.md)。

此前 STATUS（最近更新为 11:32 UTC）已[按原字节完整归档](history/STATUS-20261003-1132.md)，SHA-256 为 `2ab49f34deed0026d54a8efcf65a23442208eeb61207aaa665309d0591012079`。归档中的“当前”、路径与未跑结论只对应当时上下文；原始失败和精确历史快照没有删除。

## 当前范围

本次整理依据已公开 protocol `038f2db`、core-go `b06302c`、server `36ab16f`、mobile `09d3ce4`。运行结论仍分别绑定下列证据中的实际源码与产物，不能追认为这些最新提交的完整重跑。并行候选仅在明确标为候选的项目中列出，不算已公开能力。

| 用户流程 / 平台 | 已实际验证 | 仍缺什么 / 结果限制 |
| --- | --- | --- |
| 注册、邮箱证明与首号策略 | Node/workerd 永久首号 CAS 已验；Android 最终 C 产品完成连接、注册、邮件证明、普通登录未可信、完整新码首机初始化与 CRUD；iOS 注册/邮件证明及后续登录/初始化/变量 CRUD 分别有固定产物证据 | 真实外部邮件投递未跑；账号登录始终不等于设备可信。邮箱重置 App 入口仍待接 |
| Android 产品 → 正式 CLI3 | 最新真实产品轮完成账号→首机→变量/环境 CRUD→明确授权→原生 SPAKE2→正式 CLI3 入网、Boot/Pull、CLI 写入→App 显示；34 次实际系统认证、批准接受 1、mutation 5、Pull 74 | **整轮 FAIL 149.966 秒**：最后 logout 的额外系统认证窗口未找到。主业务通过与尾部退出失败分开记；不能称整轮 PASS或完整生命周期通过。[本轮证据](../mobile/docs/ANDROID_PRODUCT_USERFLOW_20261003.md)已公开于 `b0778e3` |
| Android 独立正式退出 | 同生产来源的新空实例最小回归 PASS，53.267 秒、9 次实际系统认证；正式退出后设备/workflow/alias 不存在，本地恢复被拒绝，正常 Activity 重启仍无材料；[证据](../mobile/docs/ANDROID_PRODUCT_LOGOUT_20261003.md)公开于 `cc22862` | 没有重复 CRUD/CLI；此前完整轮末尾失败原因仍未确定，不追认整轮通过；前台稳定只作测试排序，不能代替认证或产品忙状态证明 |
| Android 原 ID 冷启动续办 | 产品业务修复已公开于 `ecbbc62`；实际单笔响应丢失→force-stop→取消认证→恢复原ID→明确续办→正式Pull→退出整轮PASS83.527秒，18次认证及1次取消；mutationAttempts=accepted=1；[证据](../mobile/docs/ANDROID_PRODUCT_PENDING_20261003.md) | 4465取消与4467入口隐藏FAIL保留；8项新业务回归及全98项通过，双ID仅组件通过。实际AAR仍固定b094，不追认最新Go PIN/DAG或重跑完整CRUD/CLI；realVaultReady仍false |
| iOS 产品 | 固定 mobile `8a0515c` Debug 实际登录未可信；显式新首机完整重输及提交 6.464 秒；变量新增/读取/更新/删除、正常退出和本地恢复拒绝通过；init 1、mutation 3/3、Boot 8、Pull 20；[证据](../mobile/docs/IOS_PRODUCT_INITIALIZATION_CRUD_SIMULATOR.md)公开于 `91e499c` | 第一次过期流程的完成/查询均 FAIL并保留；原查询 HTTP 次数未采集，不能称已接受未知结果不可查。仅 Simulator；真机因素、iOS PIN、审批/恢复和完整生命周期未验 |
| App 锁与 PIN | Android MainActivity原生三阶段PASS；Flutter最小输入链PASS40.683秒：真实双录、独立PIN restore、错误PIN零写、正确PIN一次写且同Pull验值、UI忘记和新身份NOT_TRUSTED；[证据](../mobile/docs/PIN_FLUTTER_RUNTIME.md)随16项逐操作门控公开于 `1cb8398` | 前五FAIL保留，精确ErrPIN分类已修。账号注册/邮件/首机初始化采用同设备真实原生bootstrap，不算PIN逐屏账号/恢复码向导；CLI批准UI、恢复、迁移、V4/V5及iOS PIN未验。系统取消/临时锁定不得降级；整体realVaultReady=false |
| 首次恢复及恢复后管理手机 | 固定 Android 原生三阶段 3/3、94.475 秒、30 次系统认证、两次 force-stop，通过受限恢复→完整新码→显式 cert4 登记→CLI4；[证据](../core-go/mobilebridge/RECOVERY_NATIVE.md) | Flutter 恢复向导与普通管理产品入口仍待接线和实际验收；不能借原生通过开放所有操作 |
| 重复恢复 DAG → CLI5 | 已公开 major2/证书5/P4 客户端与服务；独立真实 A→B→C 恢复、原生 SPAKE2、正式 CLI5 RO/RW、CGO0 daemon、Boot/Pull/IPC/隔离 shell 通过。主 race 32.89 秒、Go 总计 34.356 秒；Node/workerd 8 项通过；[证据](evidence/RECOVERY-DAG-HTTP-VALIDATION.md) | B/C 是 Go API与测试加密存储适配器，不是手机 UI。手机 DAG S1 journal/CAS/owner合同已公开于 `08dcf01`，根独立442项race与vet通过；Android通用JNI Check/CAS已有限实测，仍无手机DAG跨认证ABI入口。S2a原ID冷查询已公开，根合并P4后426项race及3场景HTTPS/4事件通过；P4环境CRUD已公开，真实HPKE/PAKE及原包恢复通过；B1跨操作RAM owner/lease及P4每环境授权Go/TS已公开，根合并468项race、两项真实HTTPS主场景与Node/workerd3项通过；B2准备/完整码确认/同ID原子转换已公开，根576项race与3场景HTTPS通过；B3a 明确环境/角色/期限的原包登记已公开于 `af702df`，根最新版四包730项race及三个HTTPS场景通过；原登记本身仍未可信；B3b已接通Go高层正式Boot/P4 Pull与最后CAS激活，根两个受影响包695项race、真实HTTPS主项及独立跨实例退出负例通过，尚未接手机SDK/ABI/UI；手机P4业务journal/UI、DAG跨认证ABI、manager-reanchor和新Windows SCM仍缺 |
| 设备管理、前台授权提示 | Go/TS 管理与 Android 原生 42 次认证有固定证据；最小 pending 元数据已公开；CLI3 手动批准产品主链已实际通过 | Flutter 设备权限管理、真实前台请求去重/单提示/badge、取消/到期/撤销清理仍需闭环；不增加后台推送或伪造事件 |
| Linux 无人登录后台 | 固定 core `b094a93`/server `bd86fec`：完整内核重启、新 boot/两 unit 身份、目标用户未登录时持钥 Boot/Pull；CLI 写入与交互 Bash 刷新片段纠正/暂停/签撤销/逐 key 回退通过；[证据](evidence/LINUX-KERNEL-REPRODUCTION.md) | 限本次隔离 Ubuntu；新版POSIX整目录消失恢复已独立通过48项shell/provider测试；共享离线退出和只读入网检查已公开；Linux安装器已公开：首次VM原生4PASS/5FAIL保留，umask077窄修后新10项原生全PASS及正式空安装/启动非零/卸载通过；外层组清理仍FAIL，最终账号/组实际均缺失、旧资源不变；最新公开基线根71项race及Linux构建/vet通过。后续真实入网后的整轮Start检查FAIL，Boot/Pull未证明；正式正常Stop通过且材料保留。Type=exec及旧simple收据兼容修复已公开，根131项主机race/vet/ARM64编译通过；新增原生测试首轮4主PASS/1主FAIL，已独立确认是新测试错误要求未启动服务执行stop，测试修正与重跑待完成。新exec真实Start、有材料卸载与新版重启仍未通过，其它发行版和三OS整体未验。已有进程env只能经正式shell接入更新 |
| macOS 无人登录后台 | 同固定 core/server 的正式配对、LaunchDaemon、清会话后内核重启未登录 Boot/Pull、重启后 CLI写入下发、暂停签撤销和精确清理通过；[证据](evidence/MACOS-LAUNCHDAEMON-VALIDATION.md) | 原观察器字面 `loginwindow` 检查 FAIL保留；独立页面/UID/boot/HTTP证据支持未登录结论。安装器v7已公开：固定label stderr absence和目录枚举上限已修，根258项race PASS及vet/构建/20包消费者零测试编译通过；正式安装器VM仍未跑；源权限类bug负例修前FAIL保留。[新版POSIX终端恢复](../core-go/platform/POSIX-CLEANUP.md)已公开，根在06db加该切片独立race48/48、vet通过；[正式离线退出](../core-go/docs/OFFLINE-LOCAL-LOGOUT.md)已公开，根独立三包race163/163与vet通过；共享[只读入网检查](../core-go/docs/LOCAL-ENROLLMENT-CHECK.md)已公开，根独立218项race与vet通过；已停服卸载、中断重试和真实安装器VM仍未完成 |
| Windows 服务与用户环境 | 两 SCM服务可创建但从未启动，本轮精确资源清理及独立absence已通过；目标普通 Batch身份、同会话 SID、profile 加载/释放及权限恢复已有局部实证；最新 AccessCheck 80 次 API检查完成，目标自己的任务目录 create允许；同一隔离Windows进程内typed VARIANT调用帧9/9通过 | `ITaskFolder::RegisterTask` 仍 FAIL：HRESULT `80020009` / SCODE `80070005`。profile、AccessCheck与合成调用帧通过均未证明注册根因；password仅NULL→EMPTY的一次真实新注册仍FAIL3.305秒；调用帧、同目标身份及profile生命周期通过，精确新合成资源清理PASS2.293秒、独立absence PASS2.612秒；专项审查确认Scheduler是令牌获取的设计选择。唯一C++强类型对照尚未编译/执行，专项子代理被平台内容安全检查中止；[收敛记录](evidence/WINDOWS-PATH-REVIEW.md)；该对照保持停止；独立Windows原生72主项为71PASS/1FAIL；架构变量测试夹具窄修后仅该项PASS0.02秒、精确清理PASS，原失败保留；普通用户3项存储仍未跑。合成测试账号正常登录后停在首次“同意个人数据跨境传输”页，未接受；页面只见了解详细信息/下一步，拒绝后继续本地使用是否可行未知，等待明确决定，不以其他工具绕行。正式provider/CLI/SCM启动/无人登录Boot尚未验 |
| Docker、Workers、邮件 | Docker固定公开 `bd86fec` 独立 11/11；Node/workerd账号与同 Argon2id参数有本地实证；新增 DAG服务端8项另列；隔离 SMTP严格TLS 2/2 | Docker真实代理/手机完整链、Workers线上资源配额、真实SMTP投递未跑；CF Email Service可选接入未开始；不降低安全参数，不部署真实服务 |

POSIX根独立验证的源码/日志摘要与边界见[记录](evidence/posix-cleanup-result.json)；离线退出16文件的根复验见[记录](evidence/offline-local-logout-root-result.json)。两者均未包含安装器VM。新增[移动DAG S1根复验](evidence/mobile-dag-s1-root-result.json)和[只读入网检查根复验](evidence/local-enrollment-check-root-result.json)均为明确限定的组件验证。Android最新产品轮已有公开精确来源/产物及清理证据；Windows AccessCheck和macOS安装器仍按本轮协调复核的候选范围记录，不计作公开树整体通过。各平台测试只使用合成账号、独立资料与局部测试 CA；没有导入宿主真实 env或凭据。

新增[P4环境CRUD根复验](evidence/p4-environment-root-result.json)：Node/workerd3项、Go受影响包210项race、真实A→B→C后环境CRUD主项通过（进程55.451秒，Go包51.568秒），包含RO设备真实HPKE/PAKE、504原ID重开、保存失败与暂停删除回退。[S2a原事务查询根复验](evidence/mobile-dag-s2a-root-result.json)：合并P4后的四包426项race、vet、消费者编译及3场景HTTPS/4事件通过；不开放真实手机DAG能力。

新增[P4授权管理与B1根联合复验](evidence/grants-b1-root-result.json)：合并后的四包468项race通过（116.231秒），vet和18包消费者编译通过（零测试）；B1真实HTTPS1项通过6.587秒；P4真实HTTPS/HPKE/PAKE授权管理1项通过107.240秒。Node/workerd3项通过7.908秒；首次tsx临时socket路径过长导致启动FAIL，缩短专用临时路径后同源码同命令通过，原日志保留。B1仍为Go RAM owner/lease，P4联合仍为加密测试存储适配器，未据此开放手机SDK或全局设备撤销。

新增[B2根复验](evidence/mobile-dag-b2-root-result.json)：最新P4/B1合并快照四包576项race PASS（123.155秒），vet4.5秒、18包消费者编译2.76秒（零测试）；三个HTTPS业务子场景/四事件PASS16.405秒。独立审阅发现准备阶段可绕过旧入口门控，修前21FAIL保留；修后严格阻断旧入口的新HTTP和普通保存。23个核心文件已公开于 `dd85ad0`；这仍不开放真实手机DAG能力。

新增[Windows独立验证](evidence/windows-independent-import-result.json)：原72主项71PASS/1FAIL，合成子进程多出PROCESSOR_ARCHITECTURE；显式合成夹具修复公开于 `eef6713`，只复跑该项PASS0.02秒，原失败及单项诊断FAIL保留。SYSTEM上下文结果不能当作普通用户存储/服务验收。

Android 全 writer 槽位协调器已公开于 `85f8c6c`：[分批实测](../mobile/docs/ANDROID_SLOT_OWNER.md)保留首轮六项FAIL、元数据探针PASS及第二轮4PASS/2FAIL。nullable边界窄修后只复跑两项真实JNI，各PASS0.040秒、host0.937秒；SDK前后无凭据、精确包清理与原包集保持通过，没有新认证输入。四项旧产物PASS与两项新产物PASS不能合称同一APK六项通过。最新公开核心整合[48项race、vet、22包零测试编译](../core-go/mobilebridge/evidence/atomic-native-root-result.json)通过；AAR仍绑定原实测基线，未据此开放手机DAG。

iOS全writer候选已有六组Simulator组件及实际Go Check×2/普通Save×1通过；system-auth仍未完成。后续38.215秒与新增38.101秒诊断轮均FAIL、0次认证输入并精确清理。仅事件白名单诊断确认进入系统认证并等待到超时取消，自动化仍未确认可操作提示；不据此判定没有提示或线程根因。Go→Swift CAS仍未跑，iOS这部分仍是私有候选。

[macOS安装协调器](../core-go/macosservice/README.md)与[Linux安装协调器](../core-go/linuxinstall/README.md)源码公开于 `1b370cb`，没有发布安装包或Release。Mac根最新基线258项race PASS10.511秒；Linux根71项race PASS5.524秒。Linux [实际VM记录](../core-go/linuxinstall/VM_VALIDATION.md)保留外层FAIL与原4PASS/5FAIL，不能将原生10项和空生命周期的局部通过拼成整轮成功。

新增[B3a根复验](evidence/mobile-dag-b3a-root-result.json)：730项受影响race PASS（193.999秒）、vet通过、23包消费者仅编译（零测试），三个真实HTTPS/SQLite场景/四事件PASS（31.582秒）。明确选择Admin/RW/RO和期限、持久意图/challenge/原签包、响应丢失及CAS失败后的原ID查询均有限验证；独立审阅发现的嵌套字段别名缺口及修前两个FAIL保留。接受原包仍不赋予设备信任，不开放手机DAG能力。

[Android DAG A 分片根复验](evidence/android-native-dag-a-root-result.json)：稳定 PlatformEpoch、有限认证等待和 opaque registry 源码已公开；根在最新 B3a 上合并后 54 项 race PASS（23.680 秒）、vet PASS（3.664 秒）。独立并发审阅 Go 8 项、Kotlin 12 项通过。原候选 arm64 AAR 与 Kotlin 编译通过，真实 JNI/系统设备密码/业务接线未跑；尚未接生产认证或 Flutter，不开放 DAG 能力。

[恢复操作关闭根复验](evidence/recovery-operation-closure-root-result.json)：签名查询/原子关闭、持久同ID封锁和当前恢复会话验证的服务端与协议已公开；根51项Node/workerd PASS（24.134秒）、4项协议向量PASS、typecheck/build通过，新server上现有B3a三个HTTPS场景/四事件PASS（34.523秒）。独立审阅发现的旧轮换后收据查询缺口已修，原两个FAIL保留，修后四项通过。手机关闭操作Go journal/CAS已在后续独立切片完成，SDK/ABI/UI尚未接入，不开放手机DAG能力；不支持含关闭状态的数据回滚到旧写者。

[未可信恢复账号范围根复验](evidence/dag-account-scope-root-result.json)：新增高层CAS入口供原生桥在跨认证前保存精确账号/代际；根一个主项、三个子项race PASS（4.231秒），vet PASS。拒绝换号/换代和旧业务pending，不保存登录或恢复秘密；尚不代表手机SDK业务接线通过。

[B3b恢复设备激活根复验](evidence/mobile-dag-b3b-root-result.json)：原登记→完整来源重验→正式Boot/P4 Pull→最后CAS；695项race PASS（172主项，212.614秒）、vet PASS、真实HTTPS/SQLite/HPKE主项PASS（44.940秒）。CAS失败/回执丢失、离线到期、当前撤销及另一实例退出后的迟到结果均有限验证；保留原journal和黏性CAS要求，不开放手机DAG能力。

[Android DAG B 原生分派根复验](evidence/android-native-dag-b-root-result.json)：封闭B1/B2/S2a命令、每次SDK认证入口及清理闭锁源码已公开。最新核心上mobilebridge 51项race、内部registry 14项race和两包vet通过；原首命令因误加不存在的包整体FAIL保留。重新构建AAR通过6.859秒，Kotlin/API36通过5.154秒，host17项通过。后续固定原产物的真实SDK两组metadata合同已通过（4次认证输入、1次取消）；非空B1/B2、业务HTTP及Flutter仍未跑，能力开关继续关闭。

[手机恢复操作关闭客户端根复验](evidence/mobile-dag-closure-client-root-result.json)：已公开于 `2a149aa`，支持封存原ID的在线查询、显式关闭及whole-state CAS；关闭记录约束覆盖当前/基线/issuer来源图及最终激活，保留全部已接受历史和原journal。独立原反例在v1两个子项FAIL，v2同一测试源码两个子项PASS（8.104秒）。根首轮240秒套件超时整体FAIL（此前407事件通过），原记录保留；仅总时限改420秒后同源码468事件/82主测试PASS（302.886秒），真实HTTPS三个场景/四事件PASS（21.645秒），vet PASS（1.048秒）。SDK/ABI/UI关闭入口未接，不授予信任，不开放手机DAG能力。

[Linux启动等待根复验](evidence/linux-systemd-exec-root-result.json)：`b06302c` 新安装采用Type=exec，并按原完整摘要识别历史simple模板；Start在enable隐式reload后再次检查实际Type、receipt和停止状态。根在闭合恢复修复合并后131事件/45主测试race PASS（3.403秒）、vet PASS（0.465秒）、Linux ARM64测试编译PASS（3.301秒）。原生首轮测试的失败和真实systemd启动结果另记，编译不算VM通过。

[Android SDK有限实测](evidence/android-dag-sdk-metadata-result.json)：SDK观察v2实际PASS（9.326秒）：零界面输入、19个窗口样本、真实取消与七阶段清理通过。后续原两组metadata合同PASS（6.380秒、4.549秒，组件整轮10.930秒），4次系统认证输入和1次取消，最终官方清除、SDK无凭据、精确包卸载及原包集保持通过。固定原APK/AAR，未重跑当前HEAD整树；v3/v4窗口查询驱动及观察v1失败保留，不能追认。非空B1/B2、业务HTTP和Flutter仍未验。

macOS安装器测试的普通登录驱动已通过实际桌面观察；先前时间门槛/部分输入UNKNOWN记录保留。此结果仅解除测试账号登录阻塞，正式安装器状态检查及生命周期另记。Windows新条款保持未接受；[微软官方说明](https://www.microsoft.com/zh-cn/privacyoobe/oobe-data-transfer)接收方为美国Microsoft Corporation，实际页面后续选项未查看，不能宣称存在跳过后继续本地使用的方法。

## 最新验证如何使用

- 最新完整旧基线为 workspace `8b84acb` / core `5040921` / mobile `56ac910` / server `909d498` / protocol `de21d99`：原生 race 39/39，251.752 秒；同 server 独立 245/245及类型/构建通过。它不含后续 PIN、Windows、pending路由、当前 Flutter产品链或 DAG新切片；[原始范围保留在归档](history/STATUS-20261003-1132.md)。
- 后续 DAG独立联合是一个主项/两个子场景，加服务端8项与受影响包检查；iOS是固定8a产品实际运行；Android与PIN各自有独立包/脚本。不能把这些局部结果拼成最新五仓全套通过。
- iOS已正常 logout、恢复拒绝、停止自己的夹具和RAM助手；Linux/macOS证据分别记录真实清理。Android原整轮logout失败与夹具清理分别记录；随后独立正式退出/材料清除/正常Activity重启已通过，仍不追认旧轮结果。

## 当前 M2 优先差距

1. Android独立正式退出、单笔原ID跨kill续办与取消已实际通过；Flutter PIN最小输入链已通过，继续账号逐屏/批准入口、切服务、失权及其它未验生命周期。整体 `realVaultReady` 和未验能力继续关闭。
2. 接通手机恢复向导、重复恢复 DAG高层、P4环境/授权管理和manager-reanchor；保留完整新码重输、先受限、显式环境/角色/期限和最终下发/保存门槛。
3. 完成设备管理/前台授权提示和邮箱重置产品入口。Windows继续独立产品测试；被中止的令牌/计划任务对照单独保留阻塞；Mac/Linux正式安装器及各自 shell/多用户验收不能借单次脚本通过替代。
4. 按最终公开提交再作有界固定快照集成验证，保留历史失败。真机、真实邮件、线上Workers配额和安全审计仍是各自明确未验项，不无限阻挡其它已授权本机实现。

当前阶段不配置或触发CI/CD；自动更新及后续自动化按届时明确授权处理。正式 Tag、Release、安装包发布和真实线上部署仍须另行授权；发布签名私钥生成/上传须批准或用户安全输入，不读取旧私钥。
