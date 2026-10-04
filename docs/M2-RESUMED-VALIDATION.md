# M2 中断后续办验证

日期：2026-10-04。以下结果各自绑定源码和实际测试，不等同于最终五仓完整产品验收。执行器连接中断及恢复检查不计作产品通过或失败。

## Windows 本地存储

core-go 基线 c5866f59299cbb6838c3f753dda7aaefeb7fcf82；仅 localkeys/vault_test.go 修正测试夹具。Windows ARM64 合成普通用户实际执行受影响单项，1 主 + 9 子全部 PASS，主项 0.11 秒；无超时、截断、stderr，记录子进程已排空。

旧夹具新建文件先被受保护 ACL 检查拒绝，实际 ErrPermission 且零明文。真实 Vault.Save 创建保护正确的目标后，跨槽密文仍被认证拒绝。产品权限和加密代码未改。首轮原始两主 PASS / 一主 FAIL 保留，未重复通过项。本轮四份结果已保存，精确文件/空目录清理 PASS 4.396 秒。

已公开源码及脱敏详述见 [core-go 验证记录](https://github.com/harmonia-vault/core-go/blob/07187d934aa5b87b3a4e2874570fcb13aab48e9a/docs/WINDOWS_SLOT_BINDING_VALIDATION.md)。SCM、无人登录 Boot/Pull 和用户环境完整生命周期仍 UNRUN。

## Linux 正式卸载续办

原公开 core-go 78d7dd845307abb9566cf8a6d0711c4977841c11 安装链的新装、真实配对、Start/Boot/Pull、同一安装无人登录重启和 Stop 通过证据保留。原最终卸载 FAIL 于 linux_install_state_invalid；两项宿主复现未重现，不推断原故障原因。

本次在同一 VM、安装、收据、journal 和重启见证下调用同一正式 Uninstall，私有测试层仅加入固定阶段/错误类别观察，不修改已装 CLI、清理门槛或返回错误。实际 PASS 1.659 秒：离线退出子进程 exit0、成功 DTO 41 字节、stderr 空；最终 state、IPC、程序、CA、unit、enable-link、收据、journal 和 guard 全部缺失，unit not-found，目标 UID 进程为零，旧测试范围未变。没有再次 Start、重启、新建账号或更换 CA。

限制：原 post-reboot shell 已由旧运行收尾并退出，本次无法追认其卸载后的同 shell 原值恢复断言；该项仍 UNRUN。此前显式 release 的原值恢复、新增变量移除及无关变量保留通过，Stop 保配置通过，持久原值清理门槛也通过。这些是不同断言。测试账号清理尚待完成；原首次失败原因仍未确认。

## 前台配对请求业务

mobile 基线 b3daffee31c45d1a31257911651e6bc04fa7f9d4 上的七文件片段增加真实 PairID 提示、严格范围检查、到期隐藏、完整快照替换及后台/退出晚到结果退役。LOCAL_PROTECTION_PERSISTENCE 会清可信状态并保持清理闭锁；普通网络故障只清提示。

9 项非视觉业务测试 PASS 5.030 秒，静态分析 PASS 3.176 秒，源码秘密/个人数据检查通过。初次缓存沙箱 setup 失败保留。稳定 Actions/DTO 未改，默认 verifiedPendingPairingOperations 仍为空；组件通过不能开启真实系统认证或产品能力。对应 Android SDK、界面闭环本片未执行。

## 已授权范围

八项固定清单只对应 M2。iOS 现在并行，CI/CD 和自动更新在 M2 后继续；Tag、Release、安装包发布和真实部署不在本轮执行范围。完整交付需明确所有未验项，不将粗略进度或工期估算视为承诺。
