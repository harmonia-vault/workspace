# Windows 服务路径收敛记录

2026-10-03。Windows 产品仍未完成；本页记录已经得到的结论和一次尚未执行的对照，不能作为服务验收。

Task Scheduler 是当前取得无人登录时目标用户令牌的实现选择。同步服务与当前用户 CLI 本身不依赖它。SCM 按所配置的服务账号登录并加载该账号的 profile；虚拟服务账号的 profile 不是目标用户的 profile。[Microsoft 服务账号说明](https://learn.microsoft.com/en-us/windows/win32/services/service-user-accounts)

当前 broker 启动顺序为 `acquireUserToken → S4U task → 已鉴权管道令牌 → LoadUserProfile → 固定 Environment 子 key`。已有两项 demand-start SCM 测试服务从未启动；直接启动仍会走同一令牌依赖。`LoadUserProfile` 要求目标用户令牌，以及调用进程的 SYSTEM/管理员身份和 Backup/Restore 权限；不能传虚拟服务令牌冒充目标用户。[Microsoft LoadUserProfile](https://learn.microsoft.com/en-us/windows/win32/api/userenv/nf-userenv-loaduserprofilew)

## 已验证的失败和参数

最新一次实际普通用户注册仍 FAIL，3.305 秒。Go `IDispatch::Invoke` 返回 `0x80020009`，异常 `SCODE=0x80070005`、`WCode=0`。参数改成 password `VT_EMPTY` 后仍失败，因此不能将此前 NULL 宣称为根因。

| 参数 | 实际语义 |
| --- | --- |
| path | BSTR `Token`，仅新建，不更新既有任务 |
| xmlText | Task 1.4；目标 SID；S4U / LeastPrivilege；无 trigger；唯一固定受保护 helper、配置参数；30 秒执行上限 |
| flags | LONG `2`，TASK_CREATE |
| userId | BSTR，本机名与合成 SAM 用户名，经受保护 SID 双向核对 |
| password | by-value `VT_EMPTY=0` |
| logonType | `TASK_LOGON_S4U=2` |
| sddl | `VT_NULL=1` |

上述参数对应[官方 RegisterTask 合同](https://learn.microsoft.com/en-us/windows/win32/api/taskschd/nf-taskschd-itaskfolder-registertask)。调用者为目标普通用户的 primary token，Batch 登录类型 4、Session 0；Users/Batch 启用，Administrators 未启用。连接属性指向同一目标 SID 和本机。完整 token 权限、限制与完整性级别没有采齐，不推断额外结论。

调用使用 STA；COM 接收安全描述符为空的受保护 DACL，packet privacy 6、impersonate 3；任务文件夹 owner 为 SYSTEM，目标 ACE mask `0x1201bf`、无继承标志。原 flags1 XML 校验通过。调用帧检查不是 RPC 抓取或服务器授权决策；这些事实尚未指出具体拒绝规则。历史两次 `E_NOINTERFACE` 来自 `CoQueryProxyBlanket`，不是 `ITaskFolder::QueryInterface`。

普通用户为自身注册 S4U 在官方文档中是允许的，因此当前失败不能证明 Windows 一概禁止该方案。[Task Scheduler 安全上下文](https://learn.microsoft.com/en-us/windows/win32/taskschd/security-contexts-for-running-tasks)

## 计划对照与当前阻塞（该实验停止）

已选定的对照保持同进程、线程、held folder、身份、XML、参数及 COM 配置，只将最终 Go Invoke 改为编译器生成的 C++ 强类型 `ITaskFolder::RegisterTask`。计划静态 CGO 链接以避免增加 DLL 安装流程，使用已固定 LLVM-mingw 头文件，明确其第三方来源；新候选尚未编译或执行。

成功只说明两条调用路径存在差异，之后仍须验证实际 S4U Run、broker、用户环境与正式 CLI。若强类型调用同样拒绝，则停止 VARIANT 探针，重新审查安装和授权约束；不扩大 ACL、添加 SeTcb、保存用户系统密码或以管理员注册掩盖现有问题。QI/构建失败只算对照未完成。

准备期间，Windows 专项子代理被平台自动内容安全检查中止。这是执行器阻塞，不是新的 Windows 权限证据，也不是一次工具审批拒绝；未另开执行路径。该令牌/计划任务对照保持停止，不能改写提示或改用代理/工具绕行。Windows整体继续：与此实验实质独立的CLI、存储和环境恢复测试另列验收；SYSTEM运行不能记为普通用户DPAPI或CLI通过。其余平台继续实现。

## 清理结果

根执行器接手正常精确清理：先独立确认原任务不存在、两服务停止、相关进程为零，再保留原拒绝事实并清理本次合成账号/profile、服务、任务目录与安装文件。清理 PASS 2.293 秒；独立缺失检查 PASS 2.612 秒，核对 26 项字段。原 VM 和本次临时诊断产物保留，没有运行新注册或启动服务。[脱敏结果与原始证据 hash](windows-stage2-result.json)
