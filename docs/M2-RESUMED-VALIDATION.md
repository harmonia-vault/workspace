# M2 中断后续办验证

日期：2026-10-04。以下结果各自绑定源码和实际测试，不等同于最终五仓完整产品验收。执行器连接中断及恢复检查不计作产品通过或失败。

## Windows 本地存储

core-go 基线 c5866f59299cbb6838c3f753dda7aaefeb7fcf82；仅 localkeys/vault_test.go 修正测试夹具。Windows ARM64 合成普通用户实际执行受影响单项，1 主 + 9 子全部 PASS，主项 0.11 秒；无超时、截断、stderr，记录子进程已排空。

旧夹具新建文件先被受保护 ACL 检查拒绝，实际 ErrPermission 且零明文。真实 Vault.Save 创建保护正确的目标后，跨槽密文仍被认证拒绝。产品权限和加密代码未改。首轮原始两主 PASS / 一主 FAIL 保留，未重复通过项。本轮四份结果已保存，精确文件/空目录清理 PASS 4.396 秒。

已公开源码及脱敏详述见 [core-go 验证记录](https://github.com/harmonia-vault/core-go/blob/07187d934aa5b87b3a4e2874570fcb13aab48e9a/docs/WINDOWS_SLOT_BINDING_VALIDATION.md)。SCM、无人登录 Boot/Pull 和用户环境完整生命周期仍 UNRUN。

## Linux 正式卸载续办

原公开 core-go 78d7dd845307abb9566cf8a6d0711c4977841c11 安装链的新装、真实配对、Start/Boot/Pull、同一安装无人登录重启和 Stop 通过证据保留。原最终卸载 FAIL 于 linux_install_state_invalid；两项宿主复现未重现，不推断原故障原因。

本次在同一 VM、安装、收据、journal 和重启见证下调用同一正式 Uninstall，私有测试层仅加入固定阶段/错误类别观察，不修改已装 CLI、清理门槛或返回错误。实际 PASS 1.659 秒：离线退出子进程 exit0、成功 DTO 41 字节、stderr 空；最终 state、IPC、程序、CA、unit、enable-link、收据、journal 和 guard 全部缺失，unit not-found，目标 UID 进程为零，旧测试范围未变。没有再次 Start、重启、新建账号或更换 CA。

限制：原 post-reboot shell 已由旧运行收尾并退出，本次无法追认其卸载后的同 shell 原值恢复断言；该项仍 UNRUN。此前显式 release 的原值恢复、新增变量移除及无关变量保留通过，Stop 保配置通过，持久原值清理门槛也通过。这些是不同断言。随后独立真实 `/bin/sh` 验证 PASS 0.776 秒：仅删除片段仍保留配置，删除本次 owned 目录后恢复原值、移除新增项且保留无关项。该临时目录已移除；原 shell 的未跑断言仍保留。精确清理 PASS 0.722 秒，只有已核三个诊断对象及本次合成账号/组被清除，四个身份索引均缺失，旧失败证据保留。原首次失败原因仍未确认。完整分段结果见 [Linux 脱敏证据](https://github.com/harmonia-vault/core-go/blob/28a35f286e9ff4bafabf62c0daedca84e0530de3/linuxinstall/evidence/P5-LIFECYCLE.md)。

## 恢复后 DAG 变量业务与授权收紧保存

core-go `94a7d53344c0ae2488a5f55fb945ad0996583434` 已公开来源专用变量写入、删除、持久原 ID 查询与重试；普通入口仍拒绝该来源。完整验签拉取后的授权状态先按原 owner/hash/epoch 和原生整份 CAS 持久保存，首次拉取或 Writer 内部拉取降权后即使拒写，也不会保留旧权限。日志保存过程中明确失去设备信任的原因继续传递；取消时只允许已验的授权安全收紧投影，不推进数据序号、新值或增权。详见[核心合同](../core-go/docs/DAG-VARIABLE-BUSINESS.md)。

限定真实 HTTPS/TypeScript/SQLite/HPKE 与 AES-CAS 适配器联合测试 PASS 59.843 秒：原响应丢失后冷查询、同 ID 续办、后续覆盖与旧收据重试不重写、同 ID 输入冲突、RO 拒写、共享删除经正式拉取移除，以及 CAS 拒绝时零 mutation POST。成熟 Writer 的待办序号实际是固定单槽：未知为 `["0"]`，接受后为单个非零序号；转换器和联合测试均有实际断言。不是系统认证、JNI、原生 PAKE 或真实 Flutter 产品证明。

定向 race 检查分别通过 Writer 内部拉取失权、保存期间撤销、两类取消、两个错误合并分支、严格回调范围与实际 Writer 序号转换。首次权限测试的 initial-pull 子项通过，但原整包随后因 240 秒总预算超时仍记 FAIL；后续只分组完成剩余场景，没有把原失败改成通过。旧夹具、命令、超时和构建失败均保留。三包 vet 通过。详细分段结果见[脱敏记录](evidence/dag-variable-business-result.json)，可复现实验源码见[联合测试](../acceptance/mobile_bridge_dag_business_test.go)。

Dart 业务修订的 76 项受影响回归和独立源码复核通过，修正了未知重试必须先查询、固定序号槽，以及无效业务响应关闭旧明文显示。手机候选尚未完成新 AAR/SDK/实际 Flutter 联合验收，逐操作 verified 仍默认空。环境 CRUD、每环境授权与期限、管理者重新锚定和全局设备撤销仍须后续专用接线；本片不完成 P1 或 M2。

## 2026-10-04 首次安装失败与 iOS 入口失败

macOS 正式 P6 首次执行使用公开 core-go `b6c8f9d` 对应的已冻结安装器和原生 CLI。合成用户正常登录、公包传输、SHA 校验、stage、root 工具摘要和安装前清单均实际通过。严格目标解析接受且目标不在 disabled 映射中；正式安装驱动返回 1。原驱动将底层退出和 stderr 隐去，安装后的三个断言也可能导致相同失败，因此不能判定是否发生部分安装。现场保留，后续 HTTPS 夹具、配对、Start、Boot/Pull、重启、终端、卸载与清理均 UNRUN。正在准备固定只读状态投影，不重复安装。

Windows 标准 SCM 私有候选的四叶 stage、官方传输和完整摘要检查通过。唯一安装进程退出 1，后续目标查询确认服务数为零、两个实例目录和收据缺失。原第一次完成查询未同时保留输出，后续错误查询不可用；官方 QEMU 文档说明退出后的 guest-exec-status 会回收进程元数据，因此之后的流程改为在首次完成响应中同时保存允许的结果。[QEMU guest-exec-status](https://qemu-project.gitlab.io/qemu/interop/qemu-ga-ref.html#command-guest-exec-status)。这只是丢失诊断的可能解释，不是原安装错误的证明。

独立固定只读 helper 实际退出 0，严格 DTO 指明 `preflightPassed=false`，当前账号名查询和有效服务登录权利查询均因身份解析拒绝。SYSTEM 身份、固定计划、配置验证、服务及实例缺失、直接权利读取、来源二进制摘要通过；两个父目录为 `ABSENT_NO_CREATE`。`rightsReadCompleted=false` 时，其余 false 占位不能解释为没有授权或拒绝权利。helper 报告零变更；唯一新增的第五个公开程序经完整摘要、ACL、无 reparse 和进程检查后精确删除，原四叶复核不变。未执行第二次安装或 Start。当前代码复用了 SCM 的 `.\用户名` 简写作 SID 查询；修复将区分 [CreateServiceW 的账号形式](https://learn.microsoft.com/en-us/windows/win32/api/winsvc/nf-winsvc-createservicew)与 [LookupAccountNameW 推荐的明确域限定名](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-lookupaccountnamew)，继续绑定本机、用户类型和精确 SID。

iOS 新原生生命周期候选在固定 Simulator 的实际前置窗口检查、安装和启动通过，五秒后的应用窗口仍白屏，记为 `FAIL_PUBLIC_ENTRY_BLANK`。本轮没有执行登录、系统认证、退出或 CRUD。原 PID 的限定启动日志不足以证明 Dart 首帧或插件完成注册；不能据此断言唯一根因。当前仅修复一次性原生冷启动绑定和有界启动元数据等待，原来固定旧产物的初始化/CRUD/退出通过记录保留且不追认新候选。独立测试 HTTPS 夹具和 RAM 助手已停止。

上述实际结果的公开投影不含本机路径、VM 标识、用户 SID、账号名、凭据或原始日志，见[限定结果](evidence/platform-install-failures.json)。

## Linux 清理后只读确认与 macOS 解析修正

Linux 在 2026-10-04 06:09 UTC 进行了唯一一次固定 VM/SSH 身份下的只读复核，PASS 0.584 秒。合成账号名称及 UID、组名称及 GID 四个索引均缺失，目标 UID 进程为零，原八个正式资源路径缺失，unit not-found、MainPID 为零、cgroup 为空。没有再次清理、重启或修改 VM。原 cleanup PASS 0.722 秒单独保留；原完整 P5 FAIL 和已退役原 shell 的恢复 UNRUN 不变。见[脱敏原始结果投影](evidence/linux-final-cleanup-readonly.json)。

macOS 解析修正已公开于 core-go `b6c8f9d2d3ba333f5a66895be08755ad2c7ef697`，仅修改 `macosservice/launch_darwin.go` 及对应测试：精确接受 `true`/`disabled` 和 `false`/`enabled` 两组等价值，继续拒绝重复目标、未知字面值或额外内容。263 个受影响 race 测试事件 PASS 14.892 秒，vet PASS 3.190 秒，安装器和原生 PAKE CLI 构建分别 PASS 4.118 与 3.466 秒。构建产物没有公开发布。已有 VM 字面值观测支持这一修正，但正式安装器的配对、服务、重启、终端及卸载链仍 UNRUN；解析成功也不授权接管已有服务或修改未知覆盖项。

## 前台配对请求业务

mobile 基线 b3daffee31c45d1a31257911651e6bc04fa7f9d4 上的七文件片段增加真实 PairID 提示、严格范围检查、到期隐藏、完整快照替换及后台/退出晚到结果退役。LOCAL_PROTECTION_PERSISTENCE 会清可信状态并保持清理闭锁；普通网络故障只清提示。

9 项非视觉业务测试 PASS 5.030 秒，静态分析 PASS 3.176 秒，源码秘密/个人数据检查通过。初次缓存沙箱 setup 失败保留。稳定 Actions/DTO 未改，默认 verifiedPendingPairingOperations 仍为空；组件通过不能开启真实系统认证或产品能力。对应 Android SDK、界面闭环本片未执行。

## Android 非空恢复原生组件

固定旧快照的一项 API 34 ARM64 SDK 组件 PASS 11.029 秒，包含四次实际系统设备密码 CryptoObject、非空恢复 owner、生成新码及完整重输后密封。返回标识/摘要的格式与 pending 元数据通过核验，不冒称额外比对了底层 journal。实际代理 transition POST 为零；系统 PIN、专用包、forward、Node 与端口精确清理通过。Flutter P1 完整恢复、提交/未知续办和入网尚未由这项测试覆盖，最新整树也未据此验收。见 [固定快照与原始证据摘要](https://github.com/harmonia-vault/mobile/blob/d40b3048a7974bb62f232d005bb8eed73a921aed/docs/ANDROID_DAG_B1B2_SDK_20261004.md)。

## 邮件重置入口与原请求

已公开 Go 独立邮件入口及 Dart 协调器/七动作端口。10 个 Go 主测试、15 个测试事件的定向 race 通过，vet 通过；12 项 Dart 业务/端口测试及静态分析通过。真实 HTTPS/SQLite 验收通过：正式申请合成邮件证明，服务器接受但响应丢失后只查询同 RAM 原请求，不重复 POST；冷查询不能准备替代请求，新代际密码成功、旧密码被拒绝。测试中的本机清理为隔离 Go adapter，不能代替 Android/iOS 系统槽物理删除。真实 SMTP 送达和完整手机重置页面仍未验收，默认能力保持关闭。见 [七动作合同](https://github.com/harmonia-vault/mobile/blob/0a8ffce83b7aca64869e0ff32bc27ee5d8fdbf70/docs/ACCOUNT_RESET_CHANNEL.md)及本仓 `acceptance/account_reset_mail_test.go`。

## 手机产品路由与 Android 联合原生通道

手机路由已公开于 mobile `57388540a329ad39cf0ca4e507ef8346b07f67bf`，接入设备管理、真实配对提示、账号重置页面及主控制器。36 项受影响业务测试（含 3 项新增路由业务用例）PASS 7.178 秒，全应用静态分析 PASS 4.021 秒；没有编写 UI 单元测试。这些结果不代替实际手机操作。默认独立 verified 操作集合仍为空。详见[产品路由整合](https://github.com/harmonia-vault/mobile/blob/57388540a329ad39cf0ca4e507ef8346b07f67bf/docs/PRODUCT_ROUTE_INTEGRATION.md)。

Android 联合原生通道已公开于 mobile `951e09d286bcb744e0c7471cfb4acc7823b13876`：DAG 命令及 getter、前台配对查询、P3 七动作、原生 endpoint 范围与全部 owner 排空。审查修复了首次地址绑定早于 BUSY 拒绝，以及 DAG 取消/后台 registry Close 期间可进入新操作的空档。现在关闭工作完成后仍须主线程确认，关闭结果不明则持续拒绝新操作。

完整 Go AAR 构建 PASS 26.524 秒，真实 Java ABI 核验 PASS；API 36 的全部 native/MainActivity Kotlin 编译 PASS 24.475 秒，7 个非 UI host 程序共 60 项 PASS 0.962 秒。AAR SHA256 为 `4033f8c018b1ceacaa9bccd7ab63fbb196b29a708062629d7064f56357adfcea`，实际 Go 来源是 core-go `c5866f59299cbb6838c3f753dda7aaefeb7fcf82` 加已公开邮件入口，不能冒称最新核心整树。新联合版本的 SDK、JNI 和 Flutter P1/P2/P3 实跑均 UNRUN，未发布二进制。详见[原生整合边界](https://github.com/harmonia-vault/mobile/blob/951e09d286bcb744e0c7471cfb4acc7823b13876/docs/ANDROID_NATIVE_CHANNEL_INTEGRATION.md)。

恢复后的 DAG 读取路径已具备，但旧普通写入和管理路径会明确拒绝 DAG 状态。接下来的实现使用独立 DAG 高层业务与 P4 验证，先完成变量写删及原 ID 续办，再连接环境 CRUD 与每环境授权撤销；不放宽旧路径检查，不以只读 fullView 或变量写删代替“可管理”验收。

## 已授权范围

八项固定清单只对应 M2。iOS 现在并行，CI/CD 和自动更新在 M2 后继续；Tag、Release、安装包发布和真实部署不在本轮执行范围。完整交付需明确所有未验项，不将粗略进度或工期估算视为承诺。
