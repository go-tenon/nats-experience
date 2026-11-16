# NATS 实践经验

把 tenon 的任务队列从 NSQ 换成 NATS JetStream 过程中的测试、部署和运维经验（2026-11）。所有数据都有对应的脚本和原始结果，可以复现。

## 结论速览

**为什么换掉 NSQ**：在 2 核 1G 的服务端上，NSQ（`--mem-queue-size=0`）有三个和任务队列直接相关的问题，JetStream 都没有：

| | NSQ 1.3.0 | NATS JetStream 2.12 / 2.15 |
|---|---|---|
| broker 和 worker 同时 kill -9 | 丢失全部处理中的任务 | 不丢 |
| 20 万条延迟重试后 kill -9 | 全部丢失 | 全部恢复 |
| 大量延迟重试 | 每条约 1.8KB 常驻内存，约 57 万条时 OOM | 60 万条约 200MB |
| 消费完 600 万条后的磁盘 | 9.6G（段文件被误判为损坏后不删除） | 60KB |
| 64 个生产者同步发布 | 约 33k/s | 约 73k/s（中位数） |
| 10k/s 时端到端 p99 | 约 32ms | 约 2ms |

见 [01 NSQ 与 NATS 对比](docs/01-nsq-vs-nats.md)。

**NATS 2.15 没有比 2.12 慢**：一次测试中 2.15 看起来慢了一半，交替多轮后中位数相同。原因是测试机的后台负载，见 [02 版本对比与测量噪声](docs/02-nats-versions-and-noise.md)。

**单机容量（阿里云经济型 2 核 2G，PostgreSQL + NATS + 应用同机）**：tenon 完整链路约 **1000 任务/秒**（处理函数更新一行数据）；NATS 只占 15–30% 的 CPU，瓶颈在 PostgreSQL 和应用。压测还发现并修复了 tenon relay 的一个攒批延迟问题（1000/s 时 p99 从 481ms 降到 15ms）。见 [03 单机容量](docs/03-tenon-single-node-capacity.md)。

**部署**：单机就够用，不必一开始就上集群；`sync_interval: 1s`（`always` 在普通云盘上只有约 450 条/秒）；只监听内网，但公网 IP 经 NAT 映射，必须另用防火墙或安全组限制来源。见 [04 单机部署](docs/04-deploy-single-node.md)。

**多产品共用**：每个产品一个账号，subject、stream、凭据隔离，JetStream 资源单独限额；CPU 和磁盘 IO 不隔离。见 [05 多租户](docs/05-multi-tenancy.md)。

**迁移**：某个产品流量起来后搬到单独的 NATS。演练中应用持续发布 4902 个任务（491 个延迟），全部恰好处理一次，任务暂停处理约 7 秒，发布不受影响。见 [06 迁移](docs/06-migration.md)。

测试中踩过的坑和测量方法见 [07 坑与方法](docs/07-pitfalls.md)。

## 目录

```
docs/        经验文档
config/      多账号 NATS 配置模板
scripts/
  local/     在开发机上用 Docker 做的对比测试（服务端 2 核 1G）
  ecs/       在云主机上做的容量测试、同步策略测试、部署和迁移演练
tools/
  mqbench/   NSQ 与 JetStream 的压测程序：吞吐、端到端延迟、崩溃、延迟重投
  tenonload/ 用 tenon 框架按真实路径压测：事务中发布任务 → 复制槽 → relay → JetStream → worker
  tenonmig/  多租户和迁移演练：发布方和 worker 分开运行，用 SQL 核对丢失和重复
  schedexp/  JetStream 定时消息（Nats-Schedule）的行为实验
results/
  local/     对比测试的原始结果
  ecs/       云主机上的原始结果
```

## 复现

`tools/tenonload` 和 `tools/tenonmig` 依赖 tenon 源码，`go.mod` 中的 `replace` 指向与本仓库同级的 `../tenon`（例如 `~/code/go-tenon/tenon` 和 `~/code/go-tenon/nats-experience`）。

```bash
# 开发机上的对比测试：需要 Docker，至少 6 核（服务端绑 0、1 号核，客户端绑 2–5 号核）
(cd tools/mqbench && go build -o mqbench .)
bash scripts/local/run-nats215.sh          # 吞吐、延迟、崩溃、延迟重投、长时间运行
bash scripts/local/ab-versions.sh 2.12 2.15

# 云主机：编译 linux 版本后上传
(cd tools/tenonload && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o tenonload .)
scp tools/tenonload/tenonload scripts/ecs/capacity.sh <主机>:~
```

各脚本开头的注释写了前提和用法。
