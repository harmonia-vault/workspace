# Harmonia / 和弦

经手机批准，把凭据配置到需要它的运行环境。采用 MIT 许可证。

**当前是实验性实现，安全和平台集成尚未完成，不能用于生产凭据。** 下文区分设计目标与已验证的里程碑。

## 项目背景

Harmonia 的起点是在自建 Hermes、Grokbot 和 OpenAI dot 等 Agent 运行环境中使用 skill：这些工具经常依赖环境变量，反复手动配置很麻烦，也不希望把 Key 贴进 AI 对话后让 AI 代写。

项目希望把配置入口放到手机：用户创建环境和变量，确认客户端、授权范围与期限后，由客户端将凭据配置到本机。多设备同步是实现这一流程的手段。

它不隔离 Agent 与凭据。Agent 运行后仍可以读取自己的环境变量；拥有任意命令或文件访问权限的程序，也可能读取有权限访问的明文。目标是减少手动配置和对话中的凭据传递，不能把这解释成“Agent 无法读取 Key”。

## 设计目标与取舍

- **自托管与账号隔离**：手机和 CLI 可填写 HTTPS 服务地址；公开自托管实例可容纳多个隔离邮箱账号。
- **手机确认设备**：登录账号不等于设备可信。新设备需经过可信手机批准；每设备、每环境具有 RO、RW 或 Admin 角色，可设期限或持续到撤销。
- **独立环境与本机选择**：每环境独立加密钥匙；每台设备独立激活列表和优先级，同名高优先级覆盖，不同名合并。
- **明确的修改入口**：共享写入必须在线，由 App 或 CLI 显式提交云端，成功后经同一验证下发流更新本机；不自动上传用户直接修改的系统环境变量。导入需显式勾选。
- **离线与本机 override**：授权未过期时可使用已缓存配置；本机 override 不上传，先替换环境中的值再参与排序。云端删除变量、授权撤销或到期后停止生效。
- **可恢复的本机配置**：逐变量记录首次接管原值；停用或删除来源后回退其他来源，全部无来源时恢复原值或移除新增项，无关配置不动。
- **加密和恢复边界**：目标采用独立环境钥匙、数据 AEAD、HPKE 封套与设备/管理签名。恢复码不等于数据备份；全部设备和恢复码都丢失时，旧 vault 无法找回。邮箱重置是删除旧账号数据后重新初始化。

首版按既定范围提供 Go 核心和三平台 CLI，Flutter + Go 手机端以 Android 为先，服务端共用 TypeScript 业务，适配 Workers 与独立 Node/SQLite Docker。

客户端目标是开机后台服务加当前用户 CLI：macOS LaunchDaemon、Linux systemd、Windows Service。首版覆盖交互终端和 Windows 当前用户环境变量；GUI、容器、cron 与其他业务服务适配不在首版范围。已有进程的环境变量不能被外部强制修改，磁盘启动前的解锁也不能绕过。

Agent 运行环境可能没有 systemd 或持久存储；临时 sandbox 不能据此宣称完整支持。需要分别验证运行时、存储和交互 shell 条件，兼容限制见 [系统服务说明](docs/SERVICES.md)。

## 预期使用流程

以下是目标流程，尚未全部接通：

1. 在手机创建环境和变量，选择自托管 HTTPS 地址。
2. 在目标运行环境启动 Harmonia 客户端，生成本机设备密钥并发起配对。
3. 手机核对设备，批准环境、角色与期限。
4. 本机激活所需环境及优先级，由后台服务持续协调配置。
5. 通过 App/CLI 显式修改共享值，或通过 CLI 设置只在本机生效的 override。
6. 需要时在手机撤销授权；客户端收到撤销或离线到期后停止相关值生效。

## 当前状态

五仓库、中文设计与执行计划已公开。当前 M2 已验证真实 SPAKE2、首根初始化、多管理设备的逐环境来源证明、Go 恢复轮换、正式 CLI/后台同步及断线补漏；移动界面已有显式合成预览，Android 高层原生流程正在整合。手机批准/恢复的完整产品闭环及三平台开机服务仍有未完成门槛。

每项实际通过、失败和未跑结果见 [实现状态](docs/STATUS.md)。设计要求不能冒充已实现能力，也不能把本地测试结果当作生产安全审计。

## 本机开发

```sh
git clone --recurse-submodules https://github.com/harmonia-vault/workspace.git
cd workspace
mise install
```

按各组件说明安装依赖后，统一任务为：

```sh
mise run test
mise run acceptance
mise run check-source
```

测试只使用合成账号、钥匙和临时目录；不需要真实凭据，不修改宿主真实环境配置，不部署线上服务。

| 仓库 | 职责 |
| --- | --- |
| [protocol](https://github.com/harmonia-vault/protocol) | 协议、确定性签名编码、互操作向量 |
| [core-go](https://github.com/harmonia-vault/core-go) | 密码学、本机状态、CLI、HTTPS 同步与移动桥 |
| [mobile](https://github.com/harmonia-vault/mobile) | Flutter 手机界面与平台适配 |
| [server](https://github.com/harmonia-vault/server) | 共用 TypeScript 业务与 Node/Workers 适配 |

完整目标见 [设计基线](docs/DESIGN.md)，推进顺序见 [执行计划](docs/PLAN.md)。公开仓库只包含源码、脱敏示例、文档和合成测试数据；当前先完成 M2，实现与验收后再按已授权计划加入自动更新及 CI/CD。正式 Tag、Release、安装包发布和真实线上部署仍须另行授权。
