# 01 NSQ 与 NATS JetStream 对比

目的：tenon 的任务队列原来用 NSQ，要判断在小机器上（2 核 1G）NSQ 和 NATS JetStream 谁更适合做“标准消息队列”：持久化、至少一次投递、延迟重试、多个 worker 竞争消费。

## 测试方法

- 服务端：Docker 容器，`--cpus=2 --cpuset-cpus=0,1 --memory=1g --memory-swap=1g`，数据在本机磁盘的数据卷上。
- 客户端：`tools/mqbench`，用 `taskset` 绑在其他 4 个核上，走本机回环网络，消息 1KB。
- NSQ v1.3.0：参数与 tenon 当时的配置相同（`--mem-queue-size=0`，所有消息落盘；消费端 `output_buffer_timeout=25ms`）。
- NATS：JetStream，文件存储，WorkQueue 保留策略（确认后删除），显式确认。不比较 NATS core，它不持久化，不能算消息队列。
- 采样：从容器的 cgroup 读 CPU、进程内存（anon）和含页缓存的内存（current）。

脚本：`scripts/local/`；原始结果：`results/local/`。

> 测试机是日常使用的桌面，后台负载会让单次结果波动很大，见 [02](02-nats-versions-and-noise.md)。NSQ 与 NATS 的差距是 2–4 倍，问题是行为上的（丢失、OOM、泄漏），不受波动影响。

## 性能

| 场景 | NSQ | NATS 2.12 |
|---|---|---|
| 同步发布，1 个生产者 | 7.5k/s | 8.0k/s |
| 同步发布，16 个生产者 | 30k/s，p99 12ms | 60k/s，p99 1.3ms |
| 同步发布，64 个生产者 | 35k/s，p99 19ms | 91k/s，p99 2.7ms |
| 异步批量发布 | 50k/s | 220–280k/s |
| 排空积压（逐条确认） | 53–56k/s | 247–256k/s |
| 端到端 1k/s，p50 / p99 | 8.0 / 21ms | 0.8 / 2.6ms |
| 端到端 10k/s，p50 / p99 | 1.4 / 34ms | 0.2 / 1.9ms |
| 端到端 20k/s，p50 / p99 | 10 / 62ms | 0.24 / 18ms |

- 两边服务端 CPU 都没跑满。NSQ 的瓶颈是每条消息写两次磁盘（topic 一次，channel 一次）。
- NSQ 低速率时延迟反而高（1k/s 时 p50 约 8ms），原因是 nsqd 给消费者的输出缓冲：攒满 16KB 或等满 `output_buffer_timeout` 才发出。nsqd 允许的最小值是 25ms；设成 -1 关闭缓冲会让 nsqd 偶尔停顿数十秒（tenon 当时的注释中记录过）。默认 250ms 时最大延迟稳定在约 250ms。

## 稳定性

| 场景 | NSQ | NATS |
|---|---|---|
| 10k/s 持续 10 分钟，共 600 万条 | 不丢不重；每 10 秒窗口的 p99 在 24–48ms | 不丢不重；p99 在 1.5–2.2ms |
| 积压 300 万条（3GB）再排空 | 正常 | 正常 |
| 只有 broker 被 kill -9 或重启 | 不丢，少量重复 | 不丢，少量重复 |
| broker 和 worker 同时 kill -9（200 个 5 秒的任务在处理中） | **丢失 200 条（两次都是）** | 不丢 |
| broker 正常停止、worker 崩溃 | 不丢 | 不丢 |
| 20 万条延迟重试后 kill -9 | **全部丢失** | 全部恢复 |
| 延迟重试的内存 | 每条约 1.8KB，**约 57 万条时 OOM** | 60 万条约 200MB |
| 消费完 600 万条后的磁盘 | **9.6G** | 60KB |

### NSQ 的三个问题

1. **处理中和延迟重试的消息只在内存中。** `--mem-queue-size=0` 时，消息从磁盘队列读出、交给消费者后，磁盘队列的读位置已经前进（每 2500 条或 2 秒持久化一次）。进程异常退出（kill -9、OOM、宕机）时，这些消息不在磁盘上了；只有正常停止时 nsqd 才会把它们写回。只 kill broker 的测试测不出来，因为 worker 会把手上的任务处理完，见 [07](07-pitfalls.md)。
2. **延迟重试撑爆内存。** `REQ` 带延迟的消息保存在内存的优先队列中，1KB 的消息每条约 1.8KB。1G 内存下约 57 万条被 OOM kill，接着第 1 条让它们全部丢失。下游故障导致大量任务同时重试时正好会出现。
3. **磁盘泄漏。** nsqd 的磁盘队列读到 100MB 段文件末尾时报 EOF，把文件当作损坏，改名为 `.bad` 后跳到下一个文件，`.bad` 文件不再删除。正常负载下写入 40 万条，8 个段文件中有 7 个变成 `.bad`。数据没有丢（100 万条的消费计数完全对得上），但磁盘占用约等于消息流量的 2 倍（topic 和 channel 各一份），1k/s、1KB 时每天约 170GB。看起来是 go-diskqueue 的读文件大小在文件还在写的时候就定下了。

### JetStream 需要注意的

- **延迟 NAK 会占 MaxAckPending 的名额。** `NakWithDelay` 的消息在到期前一直算作“已投递未确认”，`MaxAckPending=1000` 时每个延迟周期最多只能延迟 1000 条，消费随之卡住。2.12 和 2.15 都是这样。tenon 的重试因此不用延迟 NAK，而是确认原消息、发布一条新的定时消息（`Nats-Schedule`，2.12 起支持）。
- **默认每 2 分钟才 fsync 一次。** 进程崩溃不丢（页缓存还在），机器断电可能丢最近的写入。见 [04](04-deploy-single-node.md) 的 `sync_interval`。

## 定时消息（Nats-Schedule）的行为

`tools/schedexp` 验证过（2.12.15）：

- WorkQueue 保留策略的 stream 可以开启 `AllowMsgSchedules`。
- 定时消息发布到一个 subject（例如 `<stream>.delay.<id>`），用 `Nats-Schedule: @at <RFC3339>` 和 `Nats-Schedule-Target` 指定时间和目标 subject；到时间后服务端把它发布到目标 subject，payload 和自定义 header 保留，并加上 `Nats-Scheduler` 等 header。
- 触发后定时消息自动删除，stream 中不留残余。
- 时间精确到秒；已经过去的时间立即触发。
- 同一个 `Nats-Msg-Id` 在去重窗口内重复发布会被丢弃。
- 同一个 subject 上的定时消息会互相替换，所以每条定时消息要用自己的 subject。
- 定时消息存在 stream 中，NATS 重启、备份和恢复后都在（见 [06](06-migration.md)）。代价是每条约 0.75KB 内存（见 [03](03-tenon-single-node-capacity.md)）。
