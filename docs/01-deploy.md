# 01 部署

## 机器

| 项目 | 要求 |
|---|---|
| 规格 | 2 核 2G 起；对延迟稳定性有要求时用独享型实例 |
| 磁盘 | ESSD，开启自动快照。容量按最大积压计算：1KB 任务每 100 万条约 1GB |
| 网络 | 与应用服务器同一地域，优先同一 VPC |
| 系统 | Ubuntu |

容量参考：2 核 2G 上 NATS 每秒处理上万条消息，CPU 不到 1 核。tenon 应用的瓶颈在 PostgreSQL 和处理函数。内存随积压和定时消息增长，定时消息每条约 0.75KB。

## 单机与集群

| 情况 | 影响 |
|---|---|
| 进程崩溃、重启、升级 | 不丢任务。任务暂停处理，发布不受影响 |
| 断电、宕机 | 丢最近约 1 秒写入的任务 |
| 磁盘损坏 | 丢 NATS 中未处理的任务，包括 `max_defer` 以内的延迟和重试 |
| 广播、在线成员 | 停机期间中断，恢复后自动恢复 |

以下情况用 3 节点集群（`queue.replicas: 3`）：

- 任务丢失后无法从业务数据补回；
- 不允许任务暂停处理。

## 安装

```bash
sudo apt-get install -y docker.io docker-compose-v2
sudo usermod -aG docker $USER            # 重新登录生效
git clone git@github.com:go-tenon/nats-experience.git ~/nats && cd ~/nats
cp nats.conf.example nats.conf && chmod 600 nats.conf
```

编辑 `nats.conf`：

- `server_name` 改为机器名，之后不能热加载修改；
- 按 [02 账号](02-accounts.md) 为每个产品和 `sys` 生成密码哈希，替换 `<bcrypt 哈希>`。

启动并检查：

```bash
docker compose up -d
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'
sudo ss -ltnp | grep -E '4222|8222'      # 4222 在 0.0.0.0，8222 在 127.0.0.1
```

`compose.yaml` 中的设置：host 网络、自动重启、数据卷 `nats-data`、日志轮转、`GOMEMLIMIT=1200MiB`。内存不是 2G 时在 `.env` 中设置 `NATS_GOMEMLIMIT`，取内存的 60–80%。

## 访问控制

- 4222 监听 `0.0.0.0`，在云平台安全组中按 IP 或网段放行应用服务器。
- 同一 VPC 的应用用内网地址连接；其他应用用公网地址，安全组只放行其 IP。
- 公网连接时用户名和密码明文传输。
- 8222（监控）只监听 127.0.0.1，不对外开放。

## 配置

```
listen: 0.0.0.0:4222
http: 127.0.0.1:8222

jetstream {
  store_dir: /data
  sync_interval: "1s"
  max_file_store: 30GB
  max_memory_store: 256MB
}
```

- `sync_interval: "1s"`：每秒同步磁盘，吞吐下降 5–10%。默认 2 分钟；`always` 在普通云盘上约 450 条/秒，不用。
- `max_file_store`：磁盘上限。各账号 `max_file` 之和不超过它。
- `max_memory_store`：实时频道的在线成员使用内存存储。
- `listen`、`jetstream` 的改动执行 `docker compose restart nats`；账号、用户、配额的改动热加载，见 [03 运维](03-operations.md#修改配置)。

## 应用配置

```yaml
nats:
  url: ${NATS_URL}   # nats://product_a:<密码>@<NATS 地址>:4222，密码用环境变量传入
queue:
  stream: jobs       # 默认值
  replicas: 1
  max_defer: 10m     # 更晚的延迟和重试存入 PostgreSQL，减少 NATS 内存和迁移量
```

`<stream>.>`（默认 `jobs.>`）只给队列用，其他消息发进去会被存入队列的 stream。
