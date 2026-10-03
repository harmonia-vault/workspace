# 功能待办总览

更新时间：2026-10-03 08:58 UTC。此页按用户可操作功能分类；组件实现或局部通过不算整个功能完成。详细历史、失败及精确测试源码范围见 [STATUS.md](STATUS.md)。

本轮公开四子库：protocol `de21d9907dd7b73636afeabee4806655aaa0a82e`；core `b094a933bf1922347b4a41ea8baaa5699ffbf5c1`；mobile `9e0a00b81ebe08b203d79ee246b434412c5a67fb`；server `bd86fec6215b6f7149234578e7bf5dc764a639c2`。本文所在workspace提交固定这些gitlink；不对并行未提交工作树作验收。

| 用户功能 | 状态 | 已有证据 | 接下来需要完成 |
| --- | --- | --- | --- |
| 五个MIT public仓库、统一开发 | 完成 | 组织仓库创建、源码推送、submodule、完整设计/计划和合成测试资料 | 持续只公开审阅源码，不公开实际钥匙、账号状态或构建产物 |
| 注册及独立邮箱验证开关 | 后端已验，App进行中 | 注册时固定要求，旧免验证不锁、required pending不绕过，公开server245/245 | 原生账号3/3和固定debug公共CA配置已公开；新空实例实际Flutter注册/邮件证明/登录链仍未跑 |
| 关闭注册时的实例首号 | 后端已验，App进行中 | Node/workerd并发与永久首号CAS已验；真实Flutter连接→默认注册→登录→注册导航通过 | 系统PIN创建/真实认证/清理前置已通过；当前注册未进入预期验证页，正在按固定错误码定位，不削弱产品认证 |
| App连接地址→登录/注册 | 导航已验，账号提交进行中 | 实际HTTPS实例信息/默认注册/切登录再返回注册通过 | 实际账号注册、验证、登录与后续初始化未闭环；proxy完成响应计数为0不证明没有网络尝试 |
| UI分层、环境详情与编辑 | C界面已验，真实业务链进行中 | 最终analyze/68测试/debugAPK通过；真实Flutter明暗12张合成截图通过并保存Library，逐图核验与预览清理通过 | 真实连接→默认注册页通过；注册/CRUD/CLI完整用户链及键盘/TalkBack/其他屏宽字号仍待验 |
| 设备列表、授权详情和前台提示 | 新需求，进行中 | Go设备权限及批准机制已测，Android42次强认证管理通过；server最小pending DTO公开255项通过 | 真请求来源、ID/代际去重、单提示、pending badge、取消/到期/撤销清理、最终角色/期限确认；不加后台推送 |
| App锁、系统认证与App PIN | 独立PIN组件已验，产品接线中 | Go wrapper6主3子race；Android真实JNI5/5与NO_SYSTEM_AUTH分类通过；8文件已公开。当前公开Flutter底座加候选AAR整包编译35秒通过 | Plugin/严格六方法合同及非视觉Dart适配进行中；PIN产品入口关闭，完整PIN批准CLI未跑；既有普通AAR未替换 |
| 忘记App PIN | 组件已验，产品入口待接 | JNI实际忘记旧slot、重新创建新设备钥匙及早期失败清理通过 | 产品入口要只清本地，重新登录仍未可信、另需授权或恢复；不删除云vault |
| 账号登录与可信设备分离 | 进行中 | Go/Android严格未可信拒绝、受限恢复门槛已测 | Flutter状态机、深链/返回/重启/切服务；原生login/restore/pending/retry四意图真实3/3且12文件公开，Flutter实际链仍待验 |
| 环境和变量CRUD | 核心已验，手机进行中 | Go/CLI真实HTTPS、HPKE/AEAD、原ID故障重试和高层环境通过；Android原生管理通过 | Flutter普通/恢复后高层操作全部接线和App kill原请求恢复 |
| 多环境本机排序、同名覆盖和override | CLI范围已验 | 合并、显式本地override、云删/失权停用、逐key原值测试通过 | 三OS实际后台及手机完整产品回归；不自动上传系统env修改 |
| 勾选导入与在线共享写入 | CLI范围已验 | 仅选中上传、只提交服务器、相同pull下发、幂等原ID测试通过 | 手机导入交互、最终产品验收；进程现有env不可外部强改 |
| 离线、暂停、期限、撤销和退出 | 核心已验，跨端进行中 | Go/CLI离线读取/在线写、期限/回拨/撤销清缓存、暂停授权、原值恢复通过 | 手机后台/退出、三OS启动和崩溃实证；未收到撤销时离线有固有限制 |
| 手机批准CLI和多管理手机 | 进行中 | 真实SPAKE2/签名/HPKE、正式CLI/daemon与RO/RW已测；Androidcert3 focused19次强认证通过 | Flutter授权向导、原生新完整回归及PIN路径；不是三OS无人登录已完成 |
| 全丢设备恢复→轮换→显式登记手机→CLI | 原生已验，Flutter进行中 | 最终真实Android3/3、94.475秒、30次强认证、两次force-stop，V4手机→正式CLI4/daemon通过；22源码公开 | Flutter向导接线及产品操作；第二次DAG恢复另列 |
| 重复恢复与恢复设备继续轮换 | 进行中 | 新DAG库、固定向量、Go62主242子与TS230全项通过，真实合成HPKE/AEAD解密 | 新HTTP/major2/原子history、Go/CLI及Android第二次恢复；不以库通过冒称产品闭环 |
| 邮箱证明账号重置 | 后端已验，App待接 | 新邮件证明、破坏性确认、generation、旧设备及会话失效测试通过 | App入口、最新首号标记不重开回归和真实邮件投递 |
| macOS系统服务无人登录启动 | 已唤醒来宾，登录入口受阻 | LaunchDaemon/UID及隔离shell组件通过；当前执行器Accessibility和屏幕捕获权限实际可用，精确UTM窗口控制与正常唤醒到锁定登录页通过 | 缺现有来宾的已授权登录凭据/测试账号入口；不猜或重置密码。guestexec不支持该后端，真实boot仍未验；旧无桌面工具/权限false记录已纠正 |
| Linux系统服务无人登录启动 | bootstrap已验，服务待验 | OrbStack隔离Boot/Pull/IPC/sh/SSH通过；新完整kernel VM将同seed只读改VirtIO后，离线精确磁盘证明NoCloud/全部阶段errors为空/正确hostname与公钥 | 本次0字节串口仅缺观测，不等同OS失败；正常重启及严格公钥匹配SSH进行中，完整kernel Harmonia服务/systemd sandbox待验 |
| Windows系统服务与用户环境 | 真实注册仍失败，安全清理中 | 原生配对通过；上一轮精确cleanup与独立absence通过；新轮真实目标Batch用户、own-token、进程COM安全初始化及validate-only通过 | 原RegisterTask flags2实际80070005，根因未证实；两服务Stopped、任务目录空、正确安装进程0。严格同轮清理准备中；正式SCM/provider/CLI/boot未验 |
| Docker自托管 | 本地范围已验 | 最新公开bd86仅archive真实Docker11/11，首号/登录/同卷重启永久标记/后续403、非root/0700/明文拒绝通过，测试资源清理 | 真实TLS代理/手机链未跑；不部署真实服务 |
| Workers自托管 | 本地范围已验 | D1目录/账号DO同Argon2；新增实例首号registry与legacy迁移真实workerd回归通过 | 最新产品链、线上CPU/内存/配额未测，不能为额度弱化参数 |
| SMTP和Cloudflare发信 | 部分通过 | 隔离SMTP严格TLS两项通过，不降级、不secret debug | 真实SMTP投递未跑；CF Email Service可选接入尚未开始，不承诺全免费 |
| 自动检查、安装更新和回滚 | 未开始实现 | 已保存更新信任/版本/渠道/限流/签名/回滚设计 | M2后按授权实现和测试，不生成或读取真实签名私钥 |
| CI/CD | 未开始实现 | 用户已授权M2后开展，计划已保存 | 先可审实现与真实检查；Tag/Release/安装包发布/真实线上部署仍未授权 |
| iOS | 平台桥已公开，完整产品待验 | 18文件公开；完整Simulator arm64与未签名iPhoneOS arm64构建通过；独立实际安全15 PASS/0 FAIL/3 UNRUN；产品安装、启动和连接页查看通过 | 实际NO_SYSTEM_AUTH完整PIN、真机认证/硬件、可信产品全流程未跑；appPinReady和realVaultReady保持false |

最新独立完整五仓快照为workspace8b84/core504/mobile56ac/server909/protocolde21：原生race39/39通过（251.752秒、0FAIL/SKIP），root同server909独立245/245及typecheck/build通过。它包含公开恢复组件及注册服务兼容性，不包含后续PIN、Windows、pending route或Flutter真实链；前一次全core307 PASS/1默认构建专属SKIP仍是旧精确源码结果。当前gitlinks更晚，不追认整个最新树已验；新组件证据分别列于STATUS。此结果不是完整安全审计或生产可用声明。

外部邮件、线上Workers配额和Mac真实boot保留精确未验证项，不无限阻挡其它已授权本机实现。公开内容仅源码、脱敏文档和合成测试。虚拟机/实验标识及私有日志不纳入此页。
