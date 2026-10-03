# 功能待办总览

更新时间：2026-10-03 11:32 UTC。此页按用户可操作功能分类；组件实现或局部通过不算整个功能完成。详细历史、失败及精确测试源码范围见 [STATUS.md](STATUS.md)。

本轮公开四子库：protocol `de21d9907dd7b73636afeabee4806655aaa0a82e`；core `b094a933bf1922347b4a41ea8baaa5699ffbf5c1`；mobile `c6df1cdf6e26cd65cc10cf462bc9d25f4783949e`；server `bd86fec6215b6f7149234578e7bf5dc764a639c2`。本文所在workspace提交固定这些gitlink；不对并行未提交工作树作验收。

| 用户功能 | 状态 | 已有证据 | 接下来需要完成 |
| --- | --- | --- | --- |
| 五个MIT public仓库、统一开发 | 完成 | 组织仓库创建、源码推送、submodule、完整设计/计划和合成测试资料 | 持续只公开审阅源码，不公开实际钥匙、账号状态或构建产物 |
| 注册及独立邮箱验证开关 | 后端已验，App实际账号段已验 | 注册策略后端已验；实际Android注册、邮箱证明、退出与普通登录通过，保持登录不等于可信 | 最终C界面与新公开核心组合完整复验继续，真实外部邮件投递未跑 |
| 关闭注册时的实例首号 | 后端及实际首机通过 | Node/workerd永久首号CAS已验；最终C空实例实际注册、完整新码重输、四次认证和最后Flutter Pull通过 | 后续完整CRUD/CLI继续；不把早期旧UI或PARTIAL失败覆盖 |
| App连接地址→登录/注册 | 实际账号及首机最后Pull已验 | 最终C HTTPS、注册、邮箱证明、普通登录未可信、新码完整重输及最后Pull通过 | 8a表单修复后实际变量及环境CRUD通过；CLI批准准备页选择器失败待修，旧FAIL保留 |
| UI分层、环境详情与编辑 | C预览及部分真实业务已验 | 真实明暗12张合成预览；8a非视觉生命周期修复5/90及analyze通过；真实变量及环境CRUD通过 | CLI批准准备页选择器不可见FAIL；完整CLI及键盘/TalkBack/屏宽字号待验 |
| 设备列表、授权详情和前台提示 | 新需求，进行中 | Go设备权限及批准机制已测，Android42次强认证管理通过；server最小pending DTO公开255项通过 | 真请求来源、ID/代际去重、单提示、pending badge、取消/到期/撤销清理、最终角色/期限确认；不加后台推送 |
| App锁、系统认证与App PIN | 原生PIN纵链通过，Flutter待验 | 实际MainActivity三阶段通过，含force-stop原请求恢复、CRUD、CLI3 PAKE/Boot/Pull/RW与忘记后未可信新身份；系统认证表单修复公开 | PIN默认业务能力仍关闭；限定已验操作的Flutter候选继续，不开放未实测恢复/迁移/V4 |
| 忘记App PIN | 原生产品通过，Flutter点击待验 | 实际MainActivity精确忘记后双录新PIN，设备ID改变且NOT_TRUSTED，再清理通过 | Flutter忘记/重建点击继续；不清system slot或云vault |
| 账号登录与可信设备分离 | 实际普通登录和首机已验 | 最终C普通登录未可信、新码完整重输、初始化与最后Pull通过；iOS注册/邮件证明后仍未可信 | 恢复向导、重启/切服务及完整跨端用户链继续；各产物证据不混合 |
| 环境和变量CRUD | 核心已验，手机进行中 | Go/CLI真实HTTPS、HPKE/AEAD、原ID故障重试和高层环境通过；Android原生管理通过 | Flutter普通/恢复后高层操作全部接线和App kill原请求恢复 |
| 多环境本机排序、同名覆盖和override | CLI范围已验 | 合并、显式本地override、云删/失权停用、逐key原值测试通过 | 三OS实际后台及手机完整产品回归；不自动上传系统env修改 |
| 勾选导入与在线共享写入 | CLI范围已验 | 仅选中上传、只提交服务器、相同pull下发、幂等原ID测试通过 | 手机导入交互、最终产品验收；进程现有env不可外部强改 |
| 离线、暂停、期限、撤销和退出 | 核心已验，跨端进行中 | Go/CLI离线读取/在线写、期限/回拨/撤销清缓存、暂停授权、原值恢复通过 | 手机后台/退出、三OS启动和崩溃实证；未收到撤销时离线有固有限制 |
| 手机批准CLI和多管理手机 | 进行中 | 真实SPAKE2/签名/HPKE、正式CLI/daemon与RO/RW已测；Androidcert3 focused19次强认证通过 | Flutter授权向导、原生新完整回归及PIN路径；不是三OS无人登录已完成 |
| 全丢设备恢复→轮换→显式登记手机→CLI | 原生已验，Flutter进行中 | 最终真实Android3/3、94.475秒、30次强认证、两次force-stop，V4手机→正式CLI4/daemon通过；22源码公开 | Flutter向导接线及产品操作；第二次DAG恢复另列 |
| 重复恢复与恢复设备继续轮换 | 进行中 | 新DAG库、固定向量、Go62主242子与TS230全项通过，真实合成HPKE/AEAD解密 | 新HTTP/major2/原子history、Go/CLI及Android第二次恢复；不以库通过冒称产品闭环 |
| 邮箱证明账号重置 | 后端已验，App待接 | 新邮件证明、破坏性确认、generation、旧设备及会话失效测试通过 | App入口、最新首号标记不重开回归和真实邮件投递 |
| macOS系统服务无人登录启动 | 真实内核纵链通过，范围有限 | 固定core b094/server bd86：正式配对、LaunchDaemon、清会话后完整重启未登录Boot/Pull、CLI写入下发、暂停签撤销与精确清理通过 | 原console Name字面观察器FAIL另列；独立UI/UID/新boot HTTP支持结论；通用安装器和更广兼容性待做 |
| Linux系统服务无人登录启动 | 完整内核纵链通过，范围有限 | 固定公开core b094/server bd86：真实配对、删除会话后正常整机重启、目标未登录Boot/Pull、CLI写入下发、SSH Bash刷新片段纠正/暂停/签撤销/逐key回退通过 | 脱敏结果与复现步骤已保存于docs/evidence；其它发行版、最终安装器及三OS整体不在该结果范围，保留旧失败 |
| Windows系统服务与用户环境 | Task Scheduler注册拒绝待定位 | 两SCM服务已创建未启动；普通Batch目标身份与同会话SID通过；新profile加载/释放及权限恢复12阶段通过 | ITaskFolder::RegisterTask仍80020009/SCODE80070005；profile成功未解决。正核具体访问权，正式provider/CLI/boot未验 |
| Docker自托管 | 本地范围已验 | 最新公开bd86仅archive真实Docker11/11，首号/登录/同卷重启永久标记/后续403、非root/0700/明文拒绝通过，测试资源清理 | 真实TLS代理/手机链未跑；不部署真实服务 |
| Workers自托管 | 本地范围已验 | D1目录/账号DO同Argon2；新增实例首号registry与legacy迁移真实workerd回归通过 | 最新产品链、线上CPU/内存/配额未测，不能为额度弱化参数 |
| SMTP和Cloudflare发信 | 部分通过 | 隔离SMTP严格TLS两项通过，不降级、不secret debug | 真实SMTP投递未跑；CF Email Service可选接入尚未开始，不承诺全免费 |
| 自动检查、安装更新和回滚 | 未开始实现 | 已保存更新信任/版本/渠道/限流/签名/回滚设计 | M2后按授权实现和测试，不生成或读取真实签名私钥 |
| CI/CD | 未开始实现 | 用户已授权M2后开展，计划已保存 | 先可审实现与真实检查；Tag/Release/安装包发布/真实线上部署仍未授权 |
| iOS | 安全桥、连接及实际注册/邮件证明部分通过 | 安全15/0/3、官方模拟认证2/0/1、配置门槛3项；真实注册/邮件证明各1/1，证据9d259c1公开；一次官方模拟匹配，设备仍未可信 | 8a独立Debug构建通过，初始化/登录/管理实际未跑；真机因素与完整PIN未验 |

最新独立完整五仓快照为workspace8b84/core504/mobile56ac/server909/protocolde21：原生race39/39通过（251.752秒、0FAIL/SKIP），root同server909独立245/245及typecheck/build通过。它包含公开恢复组件及注册服务兼容性，不包含后续PIN、Windows、pending route或Flutter真实链；前一次全core307 PASS/1默认构建专属SKIP仍是旧精确源码结果。当前gitlinks更晚，不追认整个最新树已验；新组件证据分别列于STATUS。此结果不是完整安全审计或生产可用声明。

外部邮件、线上Workers配额和Windows真实boot保留精确未验证项，不无限阻挡其它已授权本机实现。公开内容仅源码、脱敏文档和合成测试。虚拟机/实验标识及私有日志不纳入此页。
