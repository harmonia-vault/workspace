# Linux 正式离线卸载与同一 shell 恢复

本次有限组合合同 **PASS，2.267 秒**：公开 core `94a7d53344c0ae2488a5f55fb945ad0996583434` 的正式安装器完成 disabled 安装，随后调用正式离线卸载；同一个仍存活的真实 `/bin/sh` 在正式状态目录删除后调用普通 `harmonia_refresh`，恢复原变量、移除工具新增项、保留无关变量。八项正式资源缺失，unit not-found、MainPID0、cgroup空；目标进程退出后，新的合成账号、组及临时工具已精确清理。

这次测试只补原先欠缺的“正式卸载删除目录＋同一存活 shell 自动恢复”组合，未重复已通过的真实配对、Start/Boot/Pull 或无人登录重启。CLI 为 Linux ARM64、CGO0，只执行离线退出，不用它证明原生 SPAKE2。服务从未 Start，也没有 HTTP 测试服务。

测试显式生成两个变量的本地加密恢复记录。它没有账号云快照、设备、登录会话或 accepted enrollment，也没有注入设备信任。初始 AccountClosed=false，正式 helper 确实执行首次 engine.Logout 及 provider 恢复；五个账号槽的删除走“已缺失”幂等分支。真实已配对材料的删除由此前实际配对链及同收据续办证据证明，不能由本次合成数据代替。

相关生产源码可复用旧证据：安装器、Vault、本地状态及离线退出的 37 个文件，与原配对验收 core `78d7dd845307abb9566cf8a6d0711c4977841c11` 逐字一致；另核 CLI 入口、账号槽删除和 POSIX 渲染/退出/暂停共 5 个文件逐字一致。共享 secure provider 的唯一差异属于 Windows 构造器；新增 syncclient 回调不在离线退出调用栈。这不是整个当前 HEAD 全端到端重新验证。

首次新夹具尝试 **FAIL，1.009 秒**，停在合成身份创建，尚未调用产品安装器。只读确认了组已建立、用户不存在、正式资源缺失；确认本 VM 不创建 mail spool 后，去掉 useradd 中不合适的 login.defs 覆盖参数。唯一续办复用原组和相同 SHA 的工具，随后完整通过本次合同，没有重建或覆盖已完成对象。

原完整 P5 的 **FAIL 26.089 秒**、原已退役 shell 的 **UNRUN** 均保留。原离线退出失败停在 service-drained 与 cleanup-authorized 之间，五个账号槽已删除；原 helper 的具体 stderr 未保留，故首次失败根因仍未确认，不能声称已重现或修复。同收据正式卸载续办 **PASS 1.659 秒** 单独保留。本次通过不把历史整轮改写成无故障通过，也不代表生产安全门槛完成。

机器可读结果及原始证据 SHA 见 [linux-p5-offline-same-shell.json](linux-p5-offline-same-shell.json)。共享安装器管理目录及永久单 UID root 管理锁按既有产品设计保留；没有泛删或修改其它账号。
