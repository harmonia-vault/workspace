# Linux 完整内核后台服务验收

2026-10-03 在独立 Ubuntu 24.04.5 ARM64 完整内核 VM 中通过了真实注册、首次初始化、正式 CLI 的证书版本 3 SPAKE2 配对、systemd 服务完整重启、无人登录自动设备授权与拉取，以及真实 SSH 交互 Bash 的暂停和撤销回退。结果见 [linux-kernel-systemd-result.json](linux-kernel-systemd-result.json)。这是实验性平台验收，不代表生产可用或完整安全审计。

固定公开源码为 core-go `b094a933bf1922347b4a41ea8baaa5699ffbf5c1`、server `bd86fec6215b6f7149234578e7bf5dc764a639c2`、workspace `fdd8ea135b1022a9456513d164ec721d798c9ac2`。使用 `git archive`，没有混入工作目录草稿。Go 1.26.4、Node 24.16.0、pnpm 11.5.2；BoringSSL 固定 `fab96f87245d7c6b941515201843665122650b88`，原前缀 `HARMONIA_BSSL`。上游 SPAKE2 测试、Go 原生 pairing 测试、原生 CLI 和 CGO0 daemon 构建均实际通过。

复现需准备独立测试 VM、独立合成用户、受保护测试目录和回环 HTTPS/持久 SQLite 实例。不要复用真实账号或设备钥匙。VM 磁盘、账号与服务的创建需要按实际资源确认；本说明不包含可直接运行的安装或格式化脚本。

1. 用固定 BoringSSL 构建原生 CLI，启用 `harmonia_boringssl`；后台 daemon 可以用同源码的 CGO0 构建。默认没有原生库的 CLI 不能完成 SPAKE2 配对。编译并发限制只用于构建，不修改安全参数。
2. 在 guest 控制器内生成随机合成密码和管理设备钥匙，执行真实注册及首次初始化。正式 CLI 用 `login --password-stdin` 接收密码，用 `pair --certificate-version 3` 本机产生设备钥匙；控制器仅在匿名 pipe 的内存中取得短码。管理侧通过 `ApprovePairingV3` 明确选择环境及 RW/期限，执行成熟 SPAKE2、双向确认、签名授权和 HPKE。正式 CLI 必须自行保存原接受回执，不能构造假 Accepted 材料。
3. 系统后台以目标合成用户 UID 运行，独占受保护状态。公有祖先目录需要允许受保护存储按 `O_RDONLY|O_DIRECTORY` 打开；用户目录与 Vault 末级保持 0700，文件 0600。服务仅写自己的用户目录；HTTPS 服务用另一合成 UID 和独立持久目录。启用数据盘挂载与两个 systemd unit。
4. 正常启动后台后，当前用户 CLI 通过 OS 认证 IPC 激活环境，并用 stdin 显式提交合成共享变量。检查服务接受成功后经正常验签拉取下发，不能直接乐观修改本地缓存。
5. 正常停止设备服务，确认退出状态和独占 Vault 已释放，仅删除旧会话缓存，保留设备钥匙、已接受信任、配置缓存和本地激活列表。正常重启 guest 一次。正式 daemon 的续期 device session 只保留 RAM，不要求重写会话文件。
6. 重启后先以独立控制账号只读观察：内核 boot ID 改变、PID1 为 systemd、两个服务有新的 InvocationID/真实目标 UID；目标用户登录会话为零，尚未运行目标 CLI 时，同一次 boot 的 HTTPS 审计已记录 boot challenge、boot session 和 pull 成功。只看服务 active 或离线缓存不够。
7. 然后运行当前用户 CLI，验证激活、在线写入及同一正常拉取更新。本次接受序号为 6。再用固定 guest 公钥验证的真实 SSH PTY 交互 Bash：启用时纠正合成外部改值，暂停后保留外部改值。管理设备提交已签名 none 授权后，暂停的后台仍处理授权投影、清除环境；同一个 shell 刷新后恢复首次接管前原值，无关变量保持不变。本次撤销接受序号为 7。
8. 关闭当前用户 CLI 和 SSH 后，只读确认后台仍为相同进程、受保护目录权限正确、shell 片段不再含被撤销的合成值。审计只保存时间、boot ID、方法、路径和 HTTP 状态，不记录请求 body、Authorization、密码或变量值。

两个失败均保留：首次测试把公有父目录设为 0711，导致实际 CLI 无法打开受保护存储祖先；只将该公有父目录改为 0755 后，用独立新合成账号配对成功，原账号和失败证据保留。首次重启观测脚本错误要求 RAM 会话落盘；查固定公开源码后修正观测项，通过且未修改产品。

本次未扩测 zsh、macOS、Windows、iOS、加密系统磁盘解锁、GUI 或其他业务服务环境注入。已有进程的环境不能被外部强制改写；本次验证的是正式 shell hook 的逐 key 行为。服务软件钥匙采用本地用户隔离权限与 AEAD 保护，不声称始终存于硬件内。没有发布安装包、Release 或部署公网实例。
