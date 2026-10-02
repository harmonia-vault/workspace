# Harmonia / 和弦

开源多设备凭据与环境变量同步，目标采用端到端加密，支持自托管多账号隔离。MIT 许可；当前属于实验性安全软件，**不能用于生产凭据**。

本仓库用 HTTPS submodule 组织统一开发：

| 仓库 | 职责 |
| --- | --- |
| [protocol](https://github.com/harmonia-vault/protocol) | 协议、签名编码、互操作向量 |
| [core-go](https://github.com/harmonia-vault/core-go) | 密码学、本机状态、CLI、移动桥 |
| [mobile](https://github.com/harmonia-vault/mobile) | Flutter 手机界面与平台适配 |
| [server](https://github.com/harmonia-vault/server) | 共用 TypeScript 业务与 Node/Workers 适配 |

```sh
git clone --recurse-submodules https://github.com/harmonia-vault/workspace.git
cd workspace
mise run test
```

完整目标见 [设计基线](docs/DESIGN.md)，推进顺序见 [执行计划](docs/PLAN.md)，实际验证和未完成门槛见 [实现状态](docs/STATUS.md)。系统集成限制见 [系统服务说明](docs/SERVICES.md)。只公开源码、脱敏示例、文档和合成测试数据；不配置 CI/CD、不发布安装包、不部署真实线上服务。
