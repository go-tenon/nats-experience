# 01 部署单机 NATS

在一台 Ubuntu 云主机上部署多产品共用的 NATS（JetStream），给 tenon 应用的队列、广播和实时频道使用。

## 机器

| 项目 | 建议 |
|---|---|
| 规格 | 2 核 2G 起步；对延迟稳定性有要求时用独享型实例，不用经济型（共享型）实例 |
| 磁盘 | ESSD 云盘，按最坏情况下的积压规划：1KB 的任务每 100 万条约 1GB；开启自动快照 |
| 网络 | 与应用服务器在同一个 VPC 的同一个地域：worker 处理每个任务都要与 NATS 往返 |
| 系统 | Ubuntu（脚本用 apt 和 ufw） |

容量参考：2 核 2G 的机器上，NATS 每秒处理数千到上万条消息时 CPU 占用不到 1 核；tenon 应用的瓶颈通常在 PostgreSQL 和处理函数，不在 NATS。内存平时只有几十 MB，主要随积压、处理中的任务和定时消息增长（定时消息每条约 0.75KB，100 万条约 750MB）。

## 何时需要集群

单机就够用，不必一开始就上集群。tenon 的任务先写进 PostgreSQL，再由 worker 转发到 NATS，所以单机 NATS 出问题时：

| 情况 | 影响 |
|---|---|
| NATS 进程崩溃、重启、升级 | 不丢任务；期间任务暂停处理，发布不受影响，新任务留在 PostgreSQL 中，恢复后自动转发 |
| 机器断电或宕机 | 最多丢最近约 1 秒写入 NATS 的任务（`sync_interval: 1s`） |
| 磁盘损坏且无法恢复 | 当时在 NATS 中的任务丢失：等待处理、处理中、`max_defer` 以内的延迟和重试 |
| 广播、在线成员 | NATS 停止期间中断，恢复后自动恢复 |

满足下面任何一条时，改用 3 台组成集群（`queue.replicas: 3`）：

- 队列中的任务丢了无法从业务数据补回，例如支付回调、对账；
- 不能接受 NATS 维护或故障期间任务暂停处理；
- 某个产品的流量需要单独的机器（先按 [04 迁移](04-migration.md) 把它迁出去）。

## 安装

```bash
# 1. Docker
bash scripts/install-docker.sh
# 重新登录一次，让当前用户加入 docker 组

# 2. NATS：每个参数是一个产品的用户名，账号名是它的大写
bash scripts/install-nats.sh product_a product_b
```

`install-nats.sh` 可以重复运行，常用的环境变量：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `ALLOW_CIDR` | `172.16.0.0/12` | 允许访问 4222 的网段，改成实际的 VPC 网段 |
| `PRIVATE_IP` | eth0 的地址 | NATS 只监听这个地址 |
| `QUOTA_FILE` / `QUOTA_MEM` | `5GB` / `64MB` | 每个产品的 JetStream 磁盘和内存配额 |
| `NATS_IMAGE` | `nats:2.15.3-alpine` | 固定版本的镜像 |

它做的事：

- 每个用户随机生成 40 位密码，明文保存在 `/etc/nats/<用户>.password`（只有 root 可读），配置中只写 bcrypt 哈希；已有的密码沿用。另外生成运维用户 `sys`。
- 生成 `/etc/nats/nats.conf`（结构见 [`config/nats.conf.example`](../config/nats.conf.example)），已有配置先备份成带时间戳的文件。
- 第一次运行时启动容器 `nats`：host 网络、`--restart always`、`GOMEMLIMIT=1200MiB`、日志轮转，数据在数据卷 `nats-data`；之后运行时热加载配置，并检查日志确认成功。
- 配置 ufw：入站只放行 SSH，4222 只放行 `ALLOW_CIDR`。

安装后检查：

```bash
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'    # {"status":"ok"}
sudo ufw status
sudo ss -ltnp | grep -E '4222|8222'                       # 4222 只在内网 IP，8222 只在 127.0.0.1
```

内存不是 2G 时，按内存的 60–80% 调整容器的 `GOMEMLIMIT`（重建容器的命令见 [03 日常运维](03-operations.md#重建容器)）。

## 网络安全

- **只监听内网 IP 不能阻止公网访问。** 云厂商的公网 IP 经 NAT 映射到内网 IP，发往公网 IP 的流量到达主机时目标就是内网 IP。必须限制来源：
  - 在控制台把安全组设为只放行应用服务器访问 4222；
  - 主机上的 ufw（脚本已配置）只放行 VPC 网段，作为第二道保护。
- 监控端口 8222 只监听 127.0.0.1，不要对外开放。
- 跨机器通信需要加密时，为 NATS 配置 TLS。
- 从自己电脑上测试“公网连不上 4222”时，如果电脑上开着代理软件（TUN 模式），TCP 连接会被代理先接住而显示“连上了”。判断方法：对照一个肯定没有监听的端口（例如 4223），或者看连上后是否收到 NATS 的握手 `INFO {...}`。

## 配置说明

```
listen: <内网 IP>:4222
http: 127.0.0.1:8222

jetstream {
  store_dir: /data
  sync_interval: "1s"
  max_file_store: 30GB
  max_memory_store: 256MB
}
```

- **`sync_interval: "1s"`**：每秒把写入同步到磁盘一次，机器断电时最多丢最近约 1 秒的写入，吞吐只降 5–10%。默认是 2 分钟。`always`（每次写入都同步）在普通云盘上只有约 450 条/秒，不要用。
- **`max_file_store`**：防止积压把磁盘写满。所有产品的配额之和不要超过它。
- **`max_memory_store`**：tenon 的队列只用文件存储，实时频道的在线成员用内存存储。
- **`server_name`** 热加载时不能修改；`listen`、`jetstream` 段的改动需要重启容器，账号、用户、配额的改动热加载即可（见 [02](02-accounts.md)）。

## 应用的配置（tenon）

应用服务器与 NATS 在同一个 VPC 内，通过内网连接。密码用环境变量传入，不要写进代码仓库：

```yaml
nats:
  url: ${NATS_URL}   # nats://product_a:<密码>@<内网 IP>:4222

queue:
  stream: jobs       # 账号之间已经隔离，用默认值即可
  replicas: 1        # 单机只能是 1
  max_defer: 10m     # 小机器上缩短：更晚的延迟和重试存入 PostgreSQL，省 NATS 的内存，也方便以后迁移
```

查看某个产品的密码：`sudo cat /etc/nats/product_a.password`

**不要往 `<stream>.>`（默认 `jobs.>`）发布其他消息**：这个范围归队列使用，发进去的消息都会被存进队列的 stream。
