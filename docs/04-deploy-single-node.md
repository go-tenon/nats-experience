# 04 单机部署

## 要不要上集群

不必一开始就上。tenon 的任务先写进 PostgreSQL 的 WAL，再由 worker 转发到 NATS，所以单机 NATS 出问题时：

| 情况 | 影响 |
|---|---|
| NATS 进程崩溃、重启、升级 | 不丢任务（实测 kill -9）。期间任务暂停处理；发布不受影响，新任务留在复制槽中，恢复后自动转发 |
| 机器断电或宕机 | 最多丢最近一个同步间隔内写入 NATS 的任务（`sync_interval: 1s` 时约 1 秒） |
| 磁盘损坏且无法恢复 | 当时在 NATS 中的任务全部丢失：等待处理、处理中、`max_defer` 以内的延迟和重试 |
| 广播、在线成员（实时频道） | NATS 停止期间中断，恢复后自动恢复（本来就不持久化） |

满足下面任何一条再上 3 节点集群（`replicas: 3`）：

- 队列中的任务丢了无法从业务数据补回（例如支付回调、对账）；
- 不能接受 NATS 维护或故障期间任务暂停处理；
- 单机性能不够。但单机测试中 NATS 只占 15–30% 的 CPU，一般轮不到。

处理函数本来就要能重复执行；丢失时可以从业务数据中找出没完成的工作重新发布。

## 磁盘同步

JetStream 默认每 2 分钟 fsync 一次。在 2 核 2G 的 ESSD 云盘上实测（`scripts/ecs/sync-test.sh`，`results/ecs/05-sync-interval.txt`）：

| 场景 | 默认（2 分钟） | `sync_interval: 1s` | `always` |
|---|---|---|---|
| 64 个生产者同步发布 | 28.0k/s | 24.8k/s | 459/s |
| 排空积压 | 81k/s | 79k/s | 454/s |
| 每秒 3000 条的端到端 p99 | 5.0ms | 5.1ms | 跟不上 |
| 机器断电时最多丢失 | 约 2 分钟的写入 | 约 1 秒的写入 | 不丢 |

`always` 时磁盘每秒约 900 次 fsync、利用率约 90%，每条消息的写入和确认各需要一次，与并发数无关，上限约 450 条/秒。**用 `sync_interval: "1s"`**，代价 5–10%。

## 部署

`scripts/ecs/install-nats.sh` 一次完成（实验机上实测过，可以重复运行）：

```bash
bash install-nats.sh product_a product_b     # 每个参数是一个产品的用户名
```

它做的事：

- 每个用户随机生成 40 位密码，明文保存在 `/etc/nats/<用户>.password`（只有 root 可读），配置中只写 bcrypt 哈希（用 `nats server passwd` 生成，明文通过环境变量传入，不出现在命令行参数里）；已有的密码沿用。
- 生成 `/etc/nats/nats.conf`（结构见 `config/nats.conf.example`），已有配置先备份。
- 容器 `nats`：镜像固定版本、host 网络、`--restart always`、`GOMEMLIMIT=1200MiB`、日志轮转，数据在数据卷 `nats-data`。
- 已在运行时用 `docker kill -s HUP nats` 重新加载，并检查日志确认成功。
- ufw：入站只放行 SSH，4222 只放行 VPC 网段。

### 配置要点

```
listen: <内网 IP>:4222          # 只监听内网
http: 127.0.0.1:8222            # 监控端口只给本机
jetstream {
  store_dir: /data
  sync_interval: "1s"
  max_file_store: 30GB           # 防止积压写满磁盘
  max_memory_store: 256MB        # 队列只用文件存储；实时功能的在线成员用内存存储
}
```

- `server_name` 重新加载时不能修改，脚本会沿用已有的值。
- `listen`、`jetstream` 段的改动需要 `docker restart nats`；账号、用户、配额的改动 HUP 即可。

### 网络安全

**只监听内网 IP 不能阻止公网访问。** 阿里云等云厂商的公网 IP 是 NAT 映射到内网 IP 上的，发往公网 IP 的流量到达主机时目标地址就是内网 IP。必须另外限制来源：

- 安全组只放行应用服务器访问 4222；
- 主机防火墙（脚本用 ufw）只放行 VPC 网段，作为第二道保护；
- 开启认证（账号和用户）；跨机器且需要加密时配置 TLS。

应用必须和 NATS 在同一个 VPC 内：worker 处理每个任务都要与 NATS 往返，跨地域的延迟直接拖慢处理速度。

验证公网是否可达时要小心本机的代理软件（TUN 模式会先接住 TCP 连接），见 [07](07-pitfalls.md)。

### 资源

- 内存：平时十几 MB；随积压、处理中的任务和定时消息增长（定时消息约 0.75KB/条）。`GOMEMLIMIT` 设为内存的 60–80%。
- 磁盘：WorkQueue 模式下任务确认后删除，平时几乎不占空间；按最坏积压规划，1KB 的任务每 100 万条约 1GB。
- 实例：经济型 e 是共享型实例，CPU 不独享，对延迟稳定性有要求时用独享型。

## 应用的配置（tenon）

```yaml
nats:
  url: ${NATS_URL}   # nats://<用户>:<密码>@<内网 IP>:4222，密码用环境变量传入，不进代码仓库

queue:
  stream: jobs       # 有账号隔离时用默认值即可
  replicas: 1        # 单机只能是 1
  max_defer: 10m     # 小机器上缩短：更晚的延迟和重试存入 PostgreSQL，随数据库备份，也省 NATS 的内存
```

## 运维

```bash
# 状态
docker ps --filter name=nats
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'
curl -s '127.0.0.1:8222/jsz?accounts=true'            # 各账号的 JetStream 用量
docker logs --tail 100 nats

# nats 命令行：用 nats-box 容器，不需要安装
nb() { u=$1; shift; docker run --rm -i --network host -e NATS_URL=nats://<内网 IP>:4222 -e NATS_USER=$u -e NATS_PASSWORD="$(sudo cat /etc/nats/$u.password)" natsio/nats-box:latest nats "$@"; }
nb product_a stream ls
nb product_a consumer report jobs                     # 每个 topic 的积压和处理中的任务
nb product_a consumer pause jobs mail_send 1h         # 暂停一个 topic（consumer 名是 topic 把 . 换成 _）
nb sys server report jetstream                        # 全局汇总
```

- 升级：看发布说明 → 给磁盘打快照 → 用新镜像重建容器（数据在数据卷中）。
- 备份：云盘自动快照；单个产品可以用 `nats backup stream` 导出（见 [06](06-migration.md)）。
- 监控：tenon 的 `queue.relay.active`（所有实例之和为 0 时告警）、`queue.relay.lag`，队列面板中“没有 Worker 在处理”“没有注册处理函数”的 topic；主机的磁盘、内存和 `healthz`。
- 开机自启已验证：重启机器后 NATS 和防火墙规则自动恢复。
