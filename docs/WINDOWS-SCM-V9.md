# Windows SCM v9 审阅分支

`core-go` 固定到 `213843c746a9ef98efc6da8f595cad14f86b79ce`，包含标准 SCM 普通账号服务候选、v9 父目录元数据修复和中文证据。其余四仓库引用保持本分支起点不变；主分支没有改动。

[核心源码与详细验证](https://github.com/harmonia-vault/core-go/blob/213843c746a9ef98efc6da8f595cad14f86b79ce/docs/WINDOWS-SCM-V9.md)

原 v9 的真实 SCM Running/Automatic、指定普通用户、Session 0 与 profile 回读通过；后续一次空账号无人登录重启也通过：服务自行启动，普通账号交互会话与交互登录事件均为 0。整合后的两项定向 provider 测试和 Windows ARM64 候选编译通过。旧 DPAPI、安装与 ACL 证据复用，未重复跑原生矩阵。

默认候选构建门槛继续关闭。当前安装的 CGO0 v9 不包含原生 SPAKE2，完整凭据流程、持有可信设备材料后的 Boot/Pull 和 P7 仍未完成。空 vault 自动启动不能代表设备授权自动验证通过。源码与原 VM 二进制范围有明确区分，不宣传生产可用；没有 CI/CD、Release、安装包发布或线上部署。

当前 Running 实例不能直接套用只接受历史失败收据的旧升级助手。后续需要精确原生映像升级、合成 HTTPS CA 与同用户 CLI 接线；没有缺少用户真实凭据，不应索取真实凭据做测试。
