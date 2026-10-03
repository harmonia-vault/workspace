# 功能待办总览

更新时间：2026-10-03 23:00 UTC。此页按用户可操作功能分类；组件实现或局部通过不算整个功能完成。当前范围见 [STATUS.md](STATUS.md)，原始历史与失败见[完整归档](history/STATUS-20261003-1132.md)。

当前公开来源：protocol `038f2db696647eceb4b0a5716784f976a55a2646`；server `36ab16fbaadacd766548e201c05acd5e80ccb89b`；core-go `b06302c541ce36edf8828f651ae342c535428a77`；mobile `09d3ce4239fe514cf1bebd4c3efb6e743a2013b6`。每项实跑仍以证据所记固定源码/产物为界；并行候选不计作公开能力。

| 用户功能 | 状态 | 已有证据 | 接下来需要完成 |
| --- | --- | --- | --- |
| 五个MIT public仓库、统一开发 | 完成 | 组织仓库创建、源码推送、submodule、完整设计/计划和合成测试资料 | 持续只公开审阅源码，不公开实际钥匙、账号状态或构建产物 |
| 注册及独立邮箱验证开关 | 后端及两手机账号路径部分已验 | Android最终C真实注册/邮箱证明/登录未可信；iOS固定9e注册与证明各1/1，固定8a登录/首机/变量CRUD通过 | 真实外部邮件投递、完整账号生命周期和邮箱重置App入口；不混合各产物范围 |
| 关闭注册时的实例首号 | 后端与实际空实例路径已验 | Node/workerd永久首号CAS；Android/iOS空实例首号、完整新码首机初始化已有实际产品证据 | 保留并发/持久首号门槛；后续重置不得重新开放，真实投递另验 |
| App连接地址→登录/注册 | 两手机实际基础产品链已验 | Android最终C账号→首机→CRUD→CLI3主链通过；iOS连接/注册/邮箱证明及固定8a登录→首机→变量CRUD分轮通过 | Android旧整轮149.966秒末尾FAIL保留；独立正式退出53.267秒/9认证已通过；iOS过期pending产品状态缺口保留；单笔原ID冷启动/取消/续办已实际PASS83.527秒（ecbbc62）；切服务等未验生命周期继续 |
| UI分层、环境详情与编辑 | C预览与实际CRUD通过，完整体验待验 | 明暗12张合成预览；8a非视觉生命周期修复5/90及analyze；Android变量/环境CRUD和CLI回写显示、iOS变量CRUD实跑 | Android旧轮尾部logout失败保留、独立最小退出已过；PIN Flutter最小输入链已通过，账号逐屏与批准UI另验；键盘/TalkBack/屏宽字号与全生命周期仍待验 |
| 设备列表、授权详情和前台提示 | 管理组件已验，产品进行中 | Go设备管理、Android42次系统认证管理、server最小pending DTO；Android产品手动批准CLI3主链通过 | 真实请求ID/代际去重、单提示、pending badge、取消/到期/撤销清理、角色/期限管理；不加后台推送 |
| App锁、系统认证与App PIN | 原生和Flutter最小输入链通过，范围有限 | MainActivity三阶段PASS；Flutter第六轮40.683秒PASS：双录/独立PIN restore、错误PIN零写、正确PIN一次验签写、UI忘记/重建未可信；精确错误分类及16项门控公开1cb8398，[证据](../mobile/docs/PIN_FLUTTER_RUNTIME.md) | 前五FAIL保留；本轮账号初始化是实际原生bootstrap，PIN逐屏账号/完整码向导和CLI批准UI未验；恢复/迁移/V4/V5继续关闭，临时系统失败不降级 |
| 忘记App PIN | 原生及Flutter本机忘记/重建已验 | 真实UI忘记后无PIN设备材料；再次双录的新身份仍NOT_TRUSTED；云端写入计数不变。原生与Flutter证据分别保留 | 取消、后台中断、系统升级迁移及其余生命周期；不清system slot或云vault |
| 账号登录与可信设备分离 | 基础产品路径已验 | Android最终C普通登录未可信、首机最后Pull；iOS注册/证明和普通登录未可信，完整初始化后才进入环境 | 恢复/DAG向导、重启/切服务和跨端完整生命周期；不以UI bool或登录成功授信 |
| 环境和变量CRUD | 核心及手机普通路径已验，恢复后管理未完 | Android最终C变量create/edit/delete/recreate与环境create/rename/delete；iOS固定8a变量create/read/update/read/delete，mutation3/3、Pull20 | P4环境及每环境授权Go/TS已公开，真实HPKE/PAKE CRUD与角色/期限/none/原收据主项通过；手机恢复后高层、环境业务journal/UI、授权管理和manager-reanchor接线；App kill原意图恢复和iOS额外环境管理另验 |
| 多环境本机排序、同名覆盖和override | CLI范围已验 | 合并、显式本地override、云删/失权停用与逐key恢复已有测试；Linux实际交互Bash纠正/暂停/签撤销通过 | Mac独立shell接入、Windows真实provider、手机完整产品回归；不自动上传系统env修改 |
| 勾选导入与在线共享写入 | CLI范围及手机CLI回写路径已验 | 仅选中上传、服务器成功后同pull下发、原ID幂等；Android产品批准CLI3后正式CLI写入并在App显示 | 手机导入交互、三OS最终产品验收；既有进程env不能由外部强改 |
| 离线、暂停、期限、撤销和退出 | 核心及部分真实平台已验 | Go/CLI期限/回拨/撤销清缓存、暂停授权；公开Unix显式离线退出，根独立三包163/163 race及vet通过；Linux/macOS真实暂停签撤销；iOS正常退出后恢复拒绝；Android独立正式退出/材料清除/正常Activity重启PASS，[证据](../mobile/docs/ANDROID_PRODUCT_LOGOUT_20261003.md) | Android单笔原ID跨kill续办/取消已PASS83.527秒、仅一次POST；4465/4467 FAIL保留；旧整轮尾部失败根因未证，其它后台/失权与Windows实证仍缺；未收到撤销时离线有固有限制 |
| 手机批准CLI和多管理手机 | Android产品CLI3主链已验，整轮仍FAIL | 34次系统认证轮完成正式CLI3 PAKE/Boot/Pull/RW写→App显示，批准1、mutation5、Pull74；[本轮证据](../mobile/docs/ANDROID_PRODUCT_USERFLOW_20261003.md)公开于b0778e3，原focused RO/RW独立 | 149.966秒整轮在最后logout窗口失败；多管理手机完整产品、Flutter PIN路径和iOS批准未验，不代表三OS无人登录 |
| 全丢设备恢复→轮换→显式登记手机→CLI | Android原生已验，Flutter未闭环 | 固定原生3/3、94.475秒、30次认证、两次force-stop，cert4手机→正式CLI4/daemon通过；22源码公开 | Flutter受限恢复/完整新码/显式角色期限向导和实际操作；重复恢复DAG高层另列 |
| 重复恢复与恢复设备继续轮换 | HTTP/Go/正式CLI5联合通过，手机高层未接 | 公开DAG major2/证书5/P4；A→B→C及CLI5 RO/RW主race32.89秒、Go34.356秒，Node/workerd8项独立PASS | B/C是Go API与测试加密存储，不是手机UI；DAG S1 journal/CAS已公开，根独立442项race/vet通过；S2a原ID冷查询已公开，根合并P4后426项race与3场景HTTPS/4事件通过；P4环境CRUD真实加密主项通过，B1跨操作RAM owner/lease及P4授权Go/TS已公开，根联合468项race、两项HTTPS与server3项通过；B2高层准备/完整码确认/同ID转换已公开，根576项race及3场景HTTPS通过；Android全writer owner和typed atomic opener已公开，两项真实JNI定向PASS；不同APK的4项旧PASS与2项新PASS分开记；B3a 明确环境/角色/期限的原包登记已公开于 `af702df`，根最新版四包730项race及三个HTTPS场景通过；原登记本身仍未可信；B3b已接通Go高层正式Boot/P4 Pull与最后CAS激活，根两个受影响包695项race、真实HTTPS主项及独立跨实例退出负例通过，尚未接手机SDK/ABI/UI；PlatformEpoch/opaque registry A源码已公开但真实跨认证业务未验（根54项race/vet通过）；恢复操作关闭服务端/协议已公开（根51项Node/workerd、4向量和现有B3a三个HTTPS场景通过），手机关闭Go journal/CAS已公开于2a149aa，根468项race、真实HTTPS4事件与vet通过；SDK/ABI/UI仍缺；未可信账号范围CAS入口已公开并定向race/vet通过；Android封闭B1/B2/S2a原生dispatch已公开，根51+14项race/vet及最新AAR/Kotlin/17项host通过，固定原产物真实SDK两组metadata合同已通过，4次认证输入/1次取消；非空B1/B2及业务HTTP未跑；手机业务journal/UI、B3接线和manager-reanchor仍缺，iOS系统认证仍未闭环；私有候选Go CAS已有限实测通过，尚未公开 |
| 邮箱证明账号重置 | 后端已验，App待接 | 新邮件证明、破坏性确认、generation、旧设备及会话失效测试通过 | App入口、最新永久首号不重开回归和真实邮件投递 |
| macOS系统服务无人登录启动 | 真实内核纵链通过，安装器候选未实跑 | 固定b094/bd86正式配对、LaunchDaemon、清会话后完整重启未登录Boot/Pull、CLI写入、暂停签撤销和清理；独立页面/UID/boot/HTTP支持 | 原console Name字面检查FAIL保留；安装器v7源码已公开，stderr与枚举边界已修；最新基线根258项race及vet/构建通过，正式安装器VM未跑；公开POSIX切片根独立48/48 race与vet通过；源权限类负例修前FAIL保留；正式离线退出已公开且根独立163/163 race及vet通过；共享只读入网检查已公开且根独立218项race/vet通过；已停服卸载/中断重试待完成，安装器VM及多用户仍未验 |
| Linux系统服务无人登录启动 | 完整内核纵链通过，范围有限 | 固定b094/bd86真实配对、清会话后重启、未登录Boot/Pull、CLI写入、SSH Bash刷新片段纠正/暂停/签撤销/逐key回退 | 新版POSIX整目录消失恢复48项独立通过；安装器已公开，首次VM原生4PASS/5FAIL保留；umask窄修后新10项原生及正式空生命周期通过，外层组清理仍FAIL但实际账号/组已缺失且旧资源不变；最新基线根71项race/ARM64全包构建/vet通过；后续真实入网后Start整轮FAIL、Boot/Pull未证明，正常Stop已通过且材料保留；新exec/旧收据兼容源码已公开、根131项race/vet/ARM64编译PASS，新增native首轮4主PASS/1主FAIL，已确认新测试错误要求未启动服务stop，修正重跑待完成；新exec真实Start/有材料卸载/新版重启未通过；共享只读入网检查已公开；其它发行版及三OS整体未验；保留旧失败，见docs/evidence |
| Windows系统服务与用户环境 | Task Scheduler注册仍FAIL，根因未证 | 两SCM服务创建后从未启动，本轮精确清理及独立absence通过；普通Batch/同会话SID、profile加载/释放/权限恢复；80次AccessCheck API完成且自己任务目录create允许；合成typed VARIANT调用帧实际9/9通过 | password EMPTY一次实际新注册仍FAIL3.305秒、80020009/SCODE80070005；新资源精确清理及独立absence通过；Scheduler仅为当前令牌获取选择，C++强类型对照未编译/执行，平台内容安全检查中止该具体实验；独立原生72主项71PASS/1FAIL，显式合成架构变量夹具修后该项PASS0.02秒及精确清理PASS；原失败保留，普通用户3项仍未跑；正常合成账号登录后首次数据跨境传输条款未接受，等待明确决定，不能认定有跳过本地使用路径。见[收敛记录](evidence/WINDOWS-PATH-REVIEW.md)；不扩大权限、不把AccessCheck当根因证明；正式provider/CLI/SCM启动/boot未验 |
| Docker自托管 | 本地固定范围已验 | 公开bd86 archive真实Docker11/11，首号/登录/同卷重启永久标记/后续403、非root/0700/明文拒绝与清理 | 新DAG Docker整链、真实TLS代理/手机组合未跑；不部署真实服务 |
| Workers自托管 | 本地范围已验 | D1目录/账号DO同Argon2；首号registry/legacy迁移workerd回归；DAG新Node/workerd8项独立通过 | 最新完整产品链及线上CPU/内存/配额未测；不能为额度弱化参数 |
| SMTP和Cloudflare发信 | 隔离SMTP部分通过 | 严格TLS两项通过，不降级、不secret debug | 真实SMTP投递未跑；CF Email Service可选接入未开始，不承诺全免费 |
| 自动检查、安装更新和回滚 | 未开始实现 | 已保存更新信任/版本/渠道/限流/签名/回滚设计 | M2后按授权实现和测试，不生成或读取真实签名私钥 |
| CI/CD | 未开始实现 | 当前阶段明确不配置或触发，计划资料保留 | 后续按明确授权处理；Tag/Release/安装包发布/真实线上部署仍未授权 |
| iOS | Simulator首机/变量CRUD/退出已验，范围有限 | 安全桥15/0/3、官方模拟认证2/0/1及配置门槛证据独立；固定8a产品初始化6.464秒、mutation3/3、Boot8/Pull20、退出后恢复拒绝；91e499c公开 | 首次过期complete/query两FAIL及短暂入口错误保留；真机因素、PIN、跨设备批准/恢复、完整生命周期和额外环境CRUD未验 |

完整旧基线仍是 workspace8b84acb/core5040921/mobile56ac910/server909d498/protocolde21d99：原生race39/39（251.752秒）及同server245/245、typecheck/build。后续DAG是单独主项与8项服务端复验，手机产品/PIN/平台也各有自己的实际范围，不能拼成最新五仓完整通过。Android最新产品整轮尾部FAIL、Flutter PIN前五FAIL与第六最小PASS40.683秒分别保留，Windows注册拒绝与Mac安装器候选状态保留；Android完整主链/尾部FAIL证据已公开b0778e3，后续独立正式退出PASS证据公开cc22862，两轮分别记录；PIN有限门控/分类修复/证据公开1cb8398；Windows/Mac未公开候选仍以本轮协调复核为依据，不算公开能力。

外部邮件、线上Workers配额和Windows真实boot保留精确未验证项，不无限阻挡其它已授权本机实现。公开内容仅源码、脱敏文档和合成测试。虚拟机/实验标识及私有日志不纳入此页。
