# NATS 部署与运维

多产品共用的单机 NATS（JetStream），供 tenon 的队列、广播、实时频道和 RPC 使用。用 Docker Compose 运行，只有 `compose.yaml` 和 `config/nats.conf` 两个文件；每个产品一个账号（account），账号和密码写在 `nats.conf` 中，增删账号不用重启；某个产品流量变大后迁到单独的 NATS。

## 机器

Ubuntu x86_64，装好 Docker 和 Docker Compose。2 核 2G 起，ESSD 并开启自动快照，与应用服务器同一地域（优先同一 VPC）。

- CPU：2 核 2G 上 NATS 每秒处理上万条消息，CPU 不到 1 核。
- 内存：随积压和定时消息增长。消息内容存在磁盘上，内存是每条定时消息的索引和到期计时，每条约 0.75KB。tenon 的延迟任务和重试全部是定时消息，一直留在 NATS 中直到执行，100 万条约 750MB；应用会大量安排远期任务时，按条数预留内存。
- 磁盘：按最大积压计算，1KB 的任务每 100 万条约 1GB。

故障影响：

| 情况 | 影响 |
|---|---|
| 进程崩溃、重启、升级 | 不丢任务。任务暂停处理，发布不受影响 |
| 断电、宕机 | 丢最近约 1 秒写入的任务 |
| 磁盘损坏 | 丢 NATS 中未处理的任务，包括全部延迟任务和重试；只能从磁盘快照恢复到快照时的状态 |

## 安装

新建一个 `nats` 目录，放在哪里都可以。下文的文件和命令都在这个目录中：

```bash
mkdir -p nats/config && cd nats
```

### compose.yaml

```yaml
name: nats

services:
  nats:
    image: nats:2.15.3-alpine
    command: ["-c", "/etc/nats/nats.conf"]
    restart: unless-stopped
    ports:
      - "4222:4222"           # 客户端，访问来源由云平台安全组控制
      - "127.0.0.1:8222:8222" # 监控，只给本机
    volumes:
      - ./config:/etc/nats:ro # 挂载目录而不是单个文件：编辑器换掉文件后，重新加载也能读到新内容
      - ./data:/data
    environment:
      GOMEMLIMIT: 1200MiB     # 内存的 60–80%
    stop_grace_period: 30s
    ulimits:
      nofile: 65536
    logging:
      driver: journald        # 写入 systemd 日志，由主机上的 OpenObserve agent 采集
      options:
        tag: nats
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8222/healthz?js-enabled-only=true"]
      interval: 10s
      timeout: 3s
      retries: 3

  # nats 命令行：docker compose run --rm cli <参数>
  cli:
    image: natsio/nats-box:0.20.2
    profiles: [tools]
    entrypoint: ["nats"]
    working_dir: /work
    volumes:
      - ./cli:/nsc/.config/nats # context，含密码
      - ./backup:/work/backup   # 迁移时的备份
    depends_on: [nats]

  # NATS 的指标转成 Prometheus 格式，由主机上的 OpenObserve agent 抓取，见 docs/04-openobserve.md
  exporter:
    image: natsio/prometheus-nats-exporter:0.20.3
    command: ["-varz", "-connz", "-healthz", "-accstatz", "-jsz=all", "http://nats:8222"]
    restart: unless-stopped
    ports:
      - "127.0.0.1:7777:7777"
    depends_on: [nats]
```

### config/nats.conf

把账号名 `A`、用户名 `a` 换成实际的名字，每个 `<密码>` 换成 `openssl rand -hex 24` 生成的不同的随机串：

```
# 修改后执行 docker compose kill -s HUP nats 重新加载，不中断连接；server_name 不能热加载
server_name: nats-1

listen: 0.0.0.0:4222
# 监控。容器外只映射到本机
http: 0.0.0.0:8222

jetstream {
  store_dir: /data
  # 每秒同步磁盘，断电最多丢约 1 秒的写入
  sync_interval: "1s"
  # 所有账号合计的磁盘上限
  max_file_store: 30GB
  # 实时频道的在线成员使用内存存储
  max_memory_store: 256MB
}

# 每个产品一个账号（account），用户名在所有账号中唯一
system_account: SYS

accounts {
  # 系统账号：查看服务器状态
  SYS {
    users = [
      { user: admin, password: "<密码>" }
    ]
  }

  A {
    jetstream: enabled
    users = [
      { user: a, password: "<密码>" }
    ]
  }
}
```

需要更多账号时照着加一段，见 [01 账号](docs/01-accounts.md)。文件中有密码，保存后执行 `chmod 600 config/nats.conf`。

### 启动

```bash
sudo docker compose up -d --wait
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'     # {"status":"ok"}
```

- 4222 对外监听，在云平台安全组中只放行应用服务器。
- 8222（监控）只映射到本机。

`config/nats.conf` 中的设置：

| 项目 | 说明 |
|---|---|
| `sync_interval: "1s"` | 每秒同步磁盘，吞吐下降 5–10%。`always` 在普通云盘上约 450 条/秒，不用 |
| `max_file_store: 30GB` | 所有账号合计的磁盘上限。账号不设配额 |
| `max_memory_store: 256MB` | 实时频道的在线成员使用内存存储 |

`nats` 目录中会出现这些子目录：`data/` 是 JetStream 的数据，`cli/` 是命令行的 context（含密码），`backup/` 是迁移时的备份。日志写入 systemd 日志（journald），用 `sudo docker compose logs nats` 或 `journalctl -t nats` 查看，保留多久由 journald 的设置决定。

## 应用配置

```yaml
nats:
  url: nats://a:<密码>@<NATS 地址>:4222
```

`<密码>` 是 `config/nats.conf` 中这个用户的密码，应用的配置文件权限设为 600。

`jobs.>`（`queue.stream` 的 subject 范围）只给队列用。不要给 `jobs` stream 设置 `max_age`，容量上限配合 `discard: new`：否则会删掉还没执行的延迟任务，worker 发现时拒绝启动。

## 文档

- [01 账号](docs/01-accounts.md)：新增账号、用户，吊销，删除账号
- [02 运维](docs/02-operations.md)：命令行、状态、修改配置、升级、备份
- [03 迁移](docs/03-migration.md)：把一个账号迁到单独的 NATS
- [04 接入 OpenObserve](docs/04-openobserve.md)
- [05 跨账号 RPC](docs/05-cross-account-rpc.md)
