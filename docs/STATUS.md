# 实现状态与验证

记录日期：2026-10-02（UTC）。这是阶段性结果，不代表完整端到端产品完成或生产安全验收。

## M0 已完成

五个 MIT public 仓库已建立，workspace 用 HTTPS submodule 固定其余四库提交。完整中文 [设计](DESIGN.md) 与 [执行计划](PLAN.md) 已保存，主 README 采用已确认的项目背景、目标和边界。

## M1 可测试安全底座

- protocol：确定性固定字符串数组签名编码、严格字段/版本校验、设备持钥会话挑战及三套 Go/Node 互操作向量。
- Go 密码学：独立环境钥匙、XChaCha20Poly1305 数据 AEAD、标准库 HPKE、独立 Ed25519 签名、严格 SHA256 密码派生、恢复用途分离基础。
- Go 本机协调：环境优先级、本机 override、首次逐 key 原值、删除/停用回退、暂停与收到撤销、离线期限、崩溃幂等恢复；CLI 可在显式合成 fixture 模式演示。
- Go 同步库：HTTPS、登录后单次持钥挑战换设备绑定会话、固定管理公钥验证、签授权/设备签名/HPKE/AEAD、持久检查点、历史补拉和已见写入序号绑定。
- server：Node SQLite 与 Workers 每账号 SQLite DO 共用 TypeScript 业务；D1 仅路由。逐请求当前授权、期限、代际、接受序号、幂等与签名；登录 Argon2id 64 MiB/3 次/并行度 1；测试入口不进入生产构建。
- 平台：原子 POSIX fragment、sh/bash/zsh 逐 key 恢复、暂停修订、Windows SID/registry/SCM 适配和三平台服务模板。生成模板不等于真实开机服务已验收。
- 手机：中文 Flutter 界面与显式合成内存预览；默认拒绝真实安全动作。Go 原生桥、钥匙保护和可信配对尚未接通。

## 实际验证

| 检查 | 结果与范围 |
| --- | --- |
| core-go `mise run test` | 通过，M1 五个包、61 个顶层测试；CLI 7、cryptox 12、localstate 16、platform 7、syncclient 19 |
| core-go `mise run test-race` | 通过，同一 M1 源码的竞态检查 |
| core-go `mise run cross-compile` | 通过，macOS arm64、Linux amd64、Windows amd64；未发布二进制 |
| protocol `mise run test` | 通过，10/10；Go/Node 字节与 Ed25519 签名、域与编码篡改、最大数据包边界 |
| server `mise run check` | 通过，TypeScript 类型检查、Node 构建及 21/21 安全/业务测试 |
| 实际本地 workerd | 通过 DO SQLite、设备会话、序号、Argon2id 相同参数路径；不等于线上配额验收 |
| Docker 本机测试 | 镜像构建、临时持久卷重启、合成账号登录、非 root、目录 0700、拒绝远程明文监听通过；测试容器/卷已清理，镜像未发布 |
| workspace `mise run acceptance` | 通过：真实 Go 管理签/HPKE/AEAD → 测试 HTTPS → TS/SQLite → Go 固定钥验签解密；登录冒用、挑战重放、跨设备/账号、RO 写拒绝、幂等冲突、撤销与重授历史补拉 |
| macOS 交互 shell | 隔离临时目录 sh/bash/zsh 通过；未改宿主 shell |
| OrbStack Ubuntu ARM64 | 平台测试通过，zsh 缺失跳过；systemd 静态配置验证通过，未安装服务 |
| mobile `mise run analyze` | 通过，无分析问题 |
| mobile 控制层测试 | 9/9 通过；不包含 UI 单元测试 |
| Android 构建 | 尚未通过：第一次因中止缓慢 NDK 官方下载而结束，退出 130；需要完成官方 NDK 安装后重新构建 |
| Android 模拟器 | 隔离 AVD 曾实际 boot_completed=1；Mac 恢复后需重新核对进程与构建后验收 |
| 源码隐私/差异检查 | 基础扫描与人工范围检查通过，未使用真实账号/凭据；不等于完整秘密审计 |

各组件详细证据：[协议](https://github.com/harmonia-vault/protocol/blob/main/docs/TEST-EVIDENCE.md)、[服务端](https://github.com/harmonia-vault/server/blob/main/docs/TESTING.md)、[系统服务](SERVICES.md)、[手机](https://github.com/harmonia-vault/mobile/blob/main/docs/VALIDATION.md)。各仓库独立公开保存对应验证记录。

## 审查修正

合成复现发现并修复了登录会话冒用受信设备 ID、重新授权遗漏旧数据、到期/退出残留本机托管缓存、暂停 shell 仍纠正、全量历史重排已见签名写、Windows 原值类型不持久等问题。针对性回归通过；这不是生产安全审计。

## 仍未完成的产品与安全门槛

- 成熟 SPAKE2 配对、首台可信手机、完整管理授权链和历史接受时间证明。
- 受限恢复、完整新码重输、一次挑战和恢复公钥/所有封套原子切换。
- 邮件验证与邮件证明的破坏性账号重置，SMTP 真正握手/投递回归。
- 无人登录开机授权、本机 IPC、服务可读加密状态/密钥保护；三 OS boot/ACL/Windows hive/Session 0/Linux SSH 实机验收。
- Go 手机原生桥、系统设备密码/强生物认证、Android 真正构建/界面验收、iOS 构建。
- WebSocket 通知、Worker 线上 Argon2id 资源配额和容量/分页策略。

正式 enrollment、共享写入 CLI 和开机 daemon 入口仍保留拒绝执行的门槛。M1 只证明可测试底座，不代表“手机批准后自动配置凭据”的完整流程已可用。

M2 已开始成熟配对库选型、本机 IPC 和加密状态存储；未验收内容不会提前计入上述通过结果。本轮不启用 CI/CD、自动更新、不发布 Release/安装包或部署真实线上服务。

## M1 已公开的实现提交

protocol：`2c410a83178d99b87c966dd5fe6fbf56ca39943f`；core-go：`de91acb9753e9acdf89eb58069e130e70723675b`；server：`a60e269c94e7eea76e7e1ee0e7b76a15753e44a1`。M2 新包不包含在这些固定提交内。
