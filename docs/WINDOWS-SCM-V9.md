# Windows SCM v9 审阅分支

`core-go` 固定到 `b29fad11ed1dee9cd1c745bfb9bfcb2134bb5f2c`，包含标准 SCM 普通账号服务候选、v9 父目录元数据修复和中文证据。其余四仓库引用保持本分支起点不变；主分支没有改动。

[核心源码与详细验证](https://github.com/harmonia-vault/core-go/blob/b29fad11ed1dee9cd1c745bfb9bfcb2134bb5f2c/docs/WINDOWS-SCM-V9.md)

原 v9 的真实 SCM Running/Automatic、指定普通用户、Session 0 与 profile 回读通过；该次已有交互登录。整合后的两项定向 provider 测试和 Windows ARM64 候选编译通过。旧 DPAPI、安装与 ACL 证据复用，未重复跑原生矩阵。

默认候选构建门槛继续关闭。当前安装的 CGO0 v9 不包含原生 SPAKE2，完整凭据流程、设备 Boot/Pull、无人登录重启和 P7 仍未完成。源码与原 VM 二进制范围有明确区分，不宣传生产可用；没有 CI/CD、Release、安装包发布或线上部署。
