# 07 坑与方法

测试过程中踩过、差点得出错误结论的地方。

## 测量

- **桌面机器上的单次压测不可信。** 后台负载让同一场景的结果相差近一倍，一次 2.15 对 2.12 的“性能下降”就是这么来的。交替多轮、看中位数和范围，见 [02](02-nats-versions-and-noise.md)。
- **测试期间不要在同一台机器上编译。** Go 交叉编译会占满所有核，包括分给服务端的核。
- **`/tmp` 常常是 tmpfs。** fsync 在 tmpfs 上几乎不花时间，放在那里测磁盘同步相关的行为会得出错误结论。
- **按计划发送时间计算延迟。** 固定速率测试中，延迟从“这条消息本该发出的时间”算起，否则发布方跟不上时延迟会被低估（协调遗漏）。`tools/mqbench` 和 `tools/tenonload` 都这样做。
- **Docker 端口映射有代价。** docker-proxy 在 2 核机器上占约 40% 的 CPU，压测和生产都应该用 host 网络或直接在宿主机上运行。
- **区分“处理过”和“确认过”。** 只 kill broker 时，worker 仍会把手上的任务处理完，测不出 broker 丢了在途消息；要同时 kill broker 和 worker，再用新的 worker 处理，核对每个编号是否都处理过（`scripts/local/crash-joint.sh`）。
- **核对丢失和重复用业务数据。** 处理函数在业务表上计数，最后一条 SQL 统计，比数消息条数可靠（`tools/tenonmig`）。

## NATS

- **延迟 NAK 占 MaxAckPending。** `NakWithDelay` 的消息在到期前算作未确认，名额满了消费就停。需要大量延迟重试时用定时消息（`Nats-Schedule`），或者把 MaxAckPending 设得很大并接受内存占用。
- **同一个 subject 上的定时消息互相替换。** 每条定时消息要用自己的 subject（tenon 用 `<stream>.delay.<消息 ID>`）。
- **定时消息每条约 0.75KB 内存**（每条一个 subject，服务端为每个 subject 维护索引）。大量重试同时等待时要算进内存预算。
- **NumSubjects 可以用来数定时消息。** 带 subject 过滤的 stream info 中，`NumSubjects` 仍然是全部 subject 数，减去任务 subject 数就是定时消息数；subject 中的消息删完后它会减少。
- **`server_name` 不能热加载修改。** 改了以后 HUP 会失败。
- **`nats sub --timeout` 不能让订阅在收不到消息时退出。** 收不到消息时命令一直等，用外部的 `timeout` 限时。
- **`nats stream backup/restore` 已过时**，用 `nats backup stream` 和 `nats backup restore stream`。
- **`nats server report accounts` 只列出当前有连接的账号。** 各账号的存储用量用监控端口的 `/jsz?accounts=true`。

## 部署和运维

- **只监听内网不等于公网不可达。** 云厂商的公网 IP 经 NAT 映射到内网 IP，要用安全组和主机防火墙限制来源。
- **本机的代理软件会让端口测试“成功”。** TUN 模式的代理会先接住 TCP 连接。判断远端端口是否真的可达：对照一个肯定没有监听的端口，或者读服务端的握手（NATS 连上会立即发送 `INFO {...}`）。
- **`pkill -f <脚本名>` 会杀掉执行它的 shell。** 通过 `ssh host '...pkill -f xxx...'` 执行时，远程 shell 的命令行里也有这个名字。
- **容器创建的文件属于 root。** 挂载到容器中的备份目录，之后要用 sudo 删除。
- **生成密码哈希时不要把明文放在命令行参数里。** `nats server passwd` 可以从环境变量 `PASSWORD` 读取明文。
- **开 ufw 之前先放行 SSH。** `ufw allow OpenSSH` 之后再 `ufw --force enable`，并用一个新的 SSH 连接确认。

## tenon

- **relay 攒批的截止时间要从第一个任务算起**，否则任务连续到达时会攒满一批才发（已修复，tenon PR #105）。
- **发布方和 worker 共用连接池时，处理函数的数据库操作会被发布方挤占。** 单机 `start` 部署时，处理函数更新一行数据的上限约 1000/s；需要更多时把 worker 拆出去或调大连接池。
- **小机器上缩短 `max_defer`**，让更晚的延迟和重试存入 PostgreSQL，省 NATS 的内存，也减少迁移时要搬的定时消息。
