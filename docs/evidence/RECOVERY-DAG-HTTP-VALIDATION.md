# 连续恢复与正式 CLI5 联合验证

2026-10-03 的独立固定源码复验通过：A 初始化后恢复为 B，再恢复为 C；C 通过原生 SPAKE2 批准两个正式 CLI5，分别验证 RO 拒写与 RW 经正常 Pull 下发。主联合使用 race，主项 32.89 秒，Go 总计 34.356 秒；含构建的墙钟时间 45.332 秒。Node/workerd 服务端的 8 项定向测试及类型检查也独立通过。

每次恢复要求完整新码重输、一次 nonce 签名和全部当前环境的新恢复封套。响应丢失后沿原 ID/hash 查询；不会隐式重签或另取 nonce。新的受限会话即使确认旧收据也仍须显式完成自己的恢复轮换。原生加密 journal 绑定账号、代际、地址和本机 owner epoch；关闭 owner 或退出后的旧 RAM 操作不能重建 journal。

B/C 部分通过 Go 业务 API 和测试用 Vault 加密存储适配器执行；CLI 配对、受保护收据、CGO0 后台、设备 Boot、P4 验签 Pull、IPC 及隔离 shell fragment 使用正式编译路径。测试只使用合成账号、合成值和临时本机 HTTPS，不安装宿主服务或读取真实环境变量。

最初的测试字段编译错误与空状态版本错误均保留为失败记录，修正仅发生在测试中。最终服务端 8 项、受影响 Go 包 race/vet 和一条真实原生联合链分别记录范围，不把合成封套测试当作原生密码学验收。

本切片仍是实验性源码。手机恢复高层/UI、P4 环境和授权管理控制、manager-reanchor 业务及新流程的 Windows SCM 联合验证尚未完成。临时 CLI 二进制已由测试清理，没有保留二进制 hash，也没有发布安装包或 Release。

复验命令与依赖见 core-go 的 `syncclient/RECOVERY-DAG.md`；准确测试结果和源码 manifest 摘要见 [JSON 记录](recovery-dag-http-result.json)。
