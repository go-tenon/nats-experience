# NATS 部署与运维

多产品共用的单机 NATS（JetStream），供 tenon 的队列、广播、实时频道和 RPC 使用。用 Docker Compose 运行；每个产品一个账号，账号和密码写在配置文件里，增删账号不用重启；某个产品流量变大后迁到单独的 NATS。

## 机器

Ubuntu x86_64，装好 Docker 和 Docker Compose。2 核 2G 起，ESSD 并开启自动快照，与应用服务器同一地域（优先同一 VPC）。

- CPU：2 核 2G 上 NATS 每秒处理上万条消息，CPU 不到 1 核。
- 内存：随积压和定时消息增长。消息内容存在磁盘上，内存是每条定时消息的索引和到期计时，每条约 0.75KB。tenon 的延迟任务和重试全部是定时消息，一直留在 NATS 中直到执行，100 万条约 750MB；产品会大量安排远期任务时，按条数预留内存。
- 磁盘：按最大积压计算，1KB 的任务每 100 万条约 1GB。

故障影响：

| 情况 | 影响 |
|---|---|
| 进程崩溃、重启、升级 | 不丢任务。任务暂停处理，发布不受影响 |
| 断电、宕机 | 丢最近约 1 秒写入的任务 |
| 磁盘损坏 | 丢 NATS 中未处理的任务，包括全部延迟任务和重试；只能从磁盘快照恢复到快照时的状态 |

## 安装

```bash
sudo install -d -o $USER /opt/nats
git clone git@github.com:go-tenon/nats-experience.git /opt/nats && cd /opt/nats
cp config/accounts.conf.example config/accounts.conf
# 每个 CHANGE_ME 换成不同的随机密码
while grep -q CHANGE_ME config/accounts.conf; do sed -i "0,/CHANGE_ME/s//$(openssl rand -hex 24)/" config/accounts.conf; done
chmod 600 config/accounts.conf
sudo docker compose up -d --wait
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'     # {"status":"ok"}
```

示例中已经有一个产品 `PRODUCT_A`，改名或新增见 [01 账号](docs/01-accounts.md)。

- 4222 对外监听，在云平台安全组中只放行应用服务器。
- 8222（监控）只映射到本机。

## 应用配置

```yaml
nats:
  url: nats://product_a:<密码>@<NATS 地址>:4222
```

`<密码>` 是 `config/accounts.conf` 中这个用户的密码，应用的配置文件权限设为 600。

`jobs.>`（`queue.stream` 的 subject 范围）只给队列用。不要给 `jobs` stream 设置 `max_age`，容量上限配合 `discard: new`：否则会删掉还没执行的延迟任务，worker 发现时拒绝启动。

## 文件

| 文件 | 说明 |
|---|---|
| `compose.yaml` | NATS；命令行 `cli`（`tools` profile）；接入 OpenObserve 的 `exporter`、`otelcol`（`monitoring` profile） |
| `config/nats.conf` | 服务器配置，见下表 |
| `config/accounts.conf` | 账号、用户和密码，从 `accounts.conf.example` 复制，不提交 |
| `config/otelcol.yaml` | 接入 OpenObserve 时的采集配置 |
| `.env` | 可选，从 `.env.example` 复制：内存上限、OpenObserve 的地址和令牌 |
| `data/` | JetStream 的数据 |
| `cli/`、`backup/` | 命令行的 context（含密码）和迁移时的备份 |

`config/nats.conf` 中的设置：

| 项目 | 说明 |
|---|---|
| `sync_interval: "1s"` | 每秒同步磁盘，吞吐下降 5–10%。`always` 在普通云盘上约 450 条/秒，不用 |
| `max_file_store: 30GB` | 所有账号合计的磁盘上限。账号不设配额 |
| `max_memory_store: 256MB` | 实时频道的在线成员使用内存存储 |
| `GOMEMLIMIT` | 在 `.env` 中，默认 1200MiB，取内存的 60–80% |

日志用 `docker compose logs nats` 查看，单个文件 100MB，保留 10 个。

## 文档

- [01 账号](docs/01-accounts.md)：新增产品、用户，吊销，删除产品
- [02 运维](docs/02-operations.md)：命令行、状态、修改配置、升级、备份
- [03 迁移](docs/03-migration.md)：把一个产品迁到单独的 NATS
- [04 接入 OpenObserve](docs/04-openobserve.md)
- [05 跨账号 RPC](docs/05-cross-account-rpc.md)
