# 功能待办总览

更新时间：2026-10-03 03:45 UTC。此页按用户可操作功能分类；组件实现或局部通过不算整个功能完成。详细历史、失败及精确测试源码范围见 [STATUS.md](STATUS.md)。

当前公开固定提交：workspace `ce264405fdf477c53a8a417d99eb319235356edb`；protocol `de21d9907dd7b73636afeabee4806655aaa0a82e`；core `6eec463eecc0e14f18dcb0ddd29ae2bcd98cad94`；mobile `d169d74d5c7bf080d02e950e6a8147d21175cc5b`；server `ae65ff6ae6a5a34857c5fb081c63d62152daba09`。本文不是对并行未提交工作树的验收。

| 用户功能 | 状态 | 已有证据 | 接下来需要完成 |
| --- | --- | --- | --- |
| 五个MIT public仓库、统一开发 | 完成 | 组织仓库创建、源码推送、submodule、完整设计/计划和合成测试资料 | 持续只公开审阅源码，不公开实际钥匙、账号状态或构建产物 |
| 注册及独立邮箱验证开关 | 进行中 | 旧实现四组合已测；已确认global verify会锁旧免验证账号，按最新用户要求修复 | per-account注册时固定要求、旧账号不补验证、required pending不能因关开关绕过 |
| 关闭注册时的实例首号 | 新需求，进行中 | 用户确认不使用token并接受先到先得风险；原子持久设计已审 | 非独占邮件pending、首次完成唯一CAS、幂等activate、崩溃恢复、不因reset/delete零用户重开；Node/DO实际并发测试 |
| App连接地址→登录/注册 | 新需求，进行中 | 新真实instance-info合同已审；普通200不当成功 | 独立连接和注册页、HTTPS/产品/major/caps验证、未初始化默认注册、快速点击/错误/回退及服务切换隔离 |
| UI分层、环境详情与编辑 | 进行中 | 原合成七预览已交付；本轮唯一Claude Opus5.5medium调用已完成 | Codex接控制层/网关，实际运行和新状态截图；原平铺界面不算最终完成 |
| 设备列表、授权详情和前台提示 | 新需求，进行中 | Go设备权限及批准机制已测，Android42次强认证管理通过 | 真请求来源、ID/代际去重、单提示、pending badge、取消/到期/撤销清理、最终角色/期限确认；不加后台推送 |
| App锁、系统认证与App PIN | 新需求，设计中 | 系统认证敏感intent已有原生证据；PIN新软件key保护合同正在审 | 冷启动/后台遮罩，系统优先、仅无能力才PIN；随机salt/KDF/AEAD/每操作grant与持久限流；CLI批准PIN实际验收 |
| 忘记App PIN | 新需求，设计中 | 用户确认无找回，正常重登录且不得变成可信设备 | 只清本地key/login/unlock/oldtrusted，重新授权或恢复，不删云vault；无绕过测试 |
| 账号登录与可信设备分离 | 进行中 | Go/Android严格未可信拒绝、受限恢复门槛已测 | Flutter状态机、深链/返回/重启/切服务；独立loginAccount真实helper已测但尚未随产品公开 |
| 环境和变量CRUD | 核心已验，手机进行中 | Go/CLI真实HTTPS、HPKE/AEAD、原ID故障重试和高层环境通过；Android原生管理通过 | Flutter普通/恢复后高层操作全部接线和App kill原请求恢复 |
| 多环境本机排序、同名覆盖和override | CLI范围已验 | 合并、显式本地override、云删/失权停用、逐key原值测试通过 | 三OS实际后台及手机完整产品回归；不自动上传系统env修改 |
| 勾选导入与在线共享写入 | CLI范围已验 | 仅选中上传、只提交服务器、相同pull下发、幂等原ID测试通过 | 手机导入交互、最终产品验收；进程现有env不可外部强改 |
| 离线、暂停、期限、撤销和退出 | 核心已验，跨端进行中 | Go/CLI离线读取/在线写、期限/回拨/撤销清缓存、暂停授权、原值恢复通过 | 手机后台/退出、三OS启动和崩溃实证；未收到撤销时离线有固有限制 |
| 手机批准CLI和多管理手机 | 进行中 | 真实SPAKE2/签名/HPKE、正式CLI/daemon与RO/RW已测；Androidcert3 focused19次强认证通过 | Flutter授权向导、原生新完整回归及PIN路径；不是三OS无人登录已完成 |
| 全丢设备恢复→轮换→显式登记手机→CLI | 进行中 | Go/CLI真实旧码/受限/完整新码/双签/角色期限/cert4/boot/pull通过 | Android三阶段曾失败，严格原挑战时钟修复已公开，正在重跑；Flutter实际接线 |
| 重复恢复与恢复设备继续轮换 | 进行中 | 新DAG库、固定向量、Go62主242子与TS230全项通过，真实合成HPKE/AEAD解密 | 新HTTP/major2/原子history、Go/CLI及Android第二次恢复；不以库通过冒称产品闭环 |
| 邮箱证明账号重置 | 后端已验，App待接 | 新邮件证明、破坏性确认、generation、旧设备及会话失效测试通过 | App入口、最新首号标记不重开回归和真实邮件投递 |
| macOS系统服务无人登录启动 | 受阻 | LaunchDaemon/UID/本地密封状态组件及隔离shell通过 | 当前UTM后端不支持guestexec，无桌面工具；需现有guestSSH合成测试入口或正常桌面执行器。真实boot单列未验 |
| Linux系统服务无人登录启动 | 部分通过 | OrbStack新测试用户init重启后的boot/pull/IPC/sh/SSH、隔离通过 | LXC共享内核，完整VM kernel启动及systemd sandbox未验 |
| Windows系统服务与用户环境 | 进行中 | SID/DPAPI/profile生命周期组件；实际WinARM64 SPAKE2六项和GoC ABI通过 | 正式nativepairing/CLI/IPC/S4U/SCM；PS Restricted安装失败无资源创建，正在审原生安装路径；真实重启未验 |
| Docker自托管 | 本地范围已验 | 同业务NodeSQLite、持久卷重启、非root/0700、拒绝远程明文通过 | 最新注册策略持久并发回归；不部署真实服务 |
| Workers自托管 | 本地范围已验 | D1仅目录、每账号SQLiteDO、同Argon2参数、真实workerd测试通过 | 新实例首号authority及legacy迁移；线上CPU/内存/配额未测，不能为额度弱化参数 |
| SMTP和Cloudflare发信 | 部分通过 | 隔离SMTP严格TLS两项通过，不降级、不secret debug | 真实SMTP投递未跑；CF Email Service可选接入尚未开始，不承诺全免费 |
| 自动检查、安装更新和回滚 | 未开始实现 | 已保存更新信任/版本/渠道/限流/签名/回滚设计 | M2后按授权实现和测试，不生成或读取真实签名私钥 |
| CI/CD | 未开始实现 | 用户已授权M2后开展，计划已保存 | 先可审实现与真实检查；Tag/Release/安装包发布/真实线上部署仍未授权 |
| iOS | 保留兼容，未验收 | project/scheme解析通过 | 原生认证适配、构建和完整模拟器验收后续；不声称Android证据等于iOS通过 |

当前固定公开五仓源码完整联合验收39/39通过（247.939秒、0FAIL/SKIP）；同固定core全包307 PASS、1默认构建专属SKIP、0FAIL、12包通过。root同公开server独立230/230、typecheck/build通过。本快照不含正在接线的新注册规则、Flutter重构、PIN或AndroidRecovery切片。此结果不是完整安全审计或生产可用声明。

外部邮件、线上Workers配额和Mac真实boot保留精确未验证项，不无限阻挡其它已授权本机实现。公开内容仅源码、脱敏文档和合成测试。虚拟机/实验标识及私有日志不纳入此页。
