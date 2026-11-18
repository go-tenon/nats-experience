# NATS 部署与运维

多项目共用的单机 NATS（JetStream），供 tenon 的队列、广播、实时频道和 RPC 使用。用 Docker Compose 运行，只有 `compose.yaml`、`config/nats.conf` 和 `Makefile` 三个文件；每个项目一个账号（account），账号和密码写在 `nats.conf` 中，增删账号不用重启；某个项目流量变大后迁到单独的 NATS。

## 机器

Ubuntu x86_64，装好 Docker、Docker Compose 和 make。2 核 2G 起，ESSD 并开启每天自动快照，与应用服务器同一地域（优先同一 VPC）。

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

新建一个 `nats` 目录，放在哪里都可以，连同 `compose.yaml` 挂载的子目录一起创建。下文的文件和命令都在这个目录中：

```bash
mkdir -p nats/{config,data,cli,backup} && cd nats
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
      - ./data:/data          # JetStream 的数据
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

  # NATS 的指标转成 Prometheus 格式，由主机上的 OpenObserve agent 抓取
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
# 修改后执行 make reload 重新加载，不中断连接；server_name 和监听地址不能热加载
server_name: nats-1

listen: 0.0.0.0:4222
# 监控。容器外只映射到本机
http: 0.0.0.0:8222

jetstream {
  store_dir: /data
  # 每秒同步磁盘，断电最多丢约 1 秒的写入，吞吐下降 5–10%；always 在普通云盘上约 450 条/秒，不用
  sync_interval: "1s"
  # 所有账号合计的磁盘上限，账号不设配额
  max_file_store: 30GB
  # 实时频道的在线成员使用内存存储
  max_memory_store: 256MB
}

# 每个项目一个账号（account），用户名在所有账号中唯一
system_account: SYS

accounts {
  # 系统账号：查看服务器状态
  SYS {
    users = [
      { user: admin, password: "<密码>" }
    ]
  }

  A {
    # 开启 JetStream，不限额；不写时关闭
    jetstream: enabled
    users = [
      { user: a, password: "<密码>" }
    ]
  }
}
```

需要更多账号时照着 `A` 加一段。文件中有密码，保存后执行 `chmod 600 config/nats.conf`，并另外保存到离线或加密存储。

### Makefile

```makefile
# 命令行以 > 开头：复制粘贴时 Tab 变成空格也不影响
.RECIPEPREFIX = >
.PHONY: reload

# 检查 config/nats.conf 的语法，通过后让 NATS 重新加载：不重启，已有连接不断开
reload:
> sudo docker compose exec nats nats-server -t -c /etc/nats/nats.conf
# 只发送 HUP 信号，容器不会停止；输出中的 Killed 表示信号已送达
> sudo docker compose kill -s HUP nats
> sleep 1
> sudo docker compose logs --tail 3 nats
```

### 启动

```bash
sudo docker compose up -d --wait
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'     # {"status":"ok"}
```

在云平台安全组中只放行应用服务器访问 4222。日志用 `sudo docker compose logs nats` 或 `journalctl -t nats` 查看。

## 应用配置

```yaml
nats:
  url: nats://a:<密码>@<NATS 地址>:4222
```

应用的配置文件权限设为 600。

`jobs.>`（`queue.stream` 的 subject 范围）只给队列用。不要给 `jobs` stream 设置 `max_age`，容量上限配合 `discard: new`：否则会删掉还没执行的延迟任务，worker 发现时拒绝启动。

## 命令行

`nats` 命令行在 `cli` 容器中运行，不用安装。在 `nats` 目录中设置别名，之后在任何目录都能用：

```bash
echo "alias nats='sudo docker compose -f $PWD/compose.yaml run --rm -q cli'" >> ~/.bashrc && . ~/.bashrc
```

为系统账号和其他每个账号各建一个 context，只需建一次：

```bash
nats context add sys --server nats://nats:4222 --user admin --password <密码>
nats context add a --server nats://nats:4222 --user a --password <密码>
```

## 账号

账号之间 subject、stream、凭据隔离；磁盘、CPU、IO 共用，某个账号影响其他账号时按 [迁移](#迁移) 迁出。同一项目的测试和正式环境用两个账号。

一个账号下的多个应用各用一个用户，便于单独吊销。吊销（凭据泄露、停用）：删掉这个用户，`make reload`，使用它的连接立即断开，重连被拒绝。换密码同理，改完再更新应用的配置。

查看：

```bash
nats --context sys server report accounts
nats --context sys server report jetstream
```

### 删除账号

逐个删除账号中的 stream，再从 `config/nats.conf` 中删掉这个账号，`make reload`，最后删除它的数据目录和 context：

```bash
nats --context c stream ls
nats --context c stream rm jobs -f
sudo rm -rf data/jetstream/C
nats context rm c -f
```

## 运维

状态：

```bash
sudo docker compose ps                    # nats 为 healthy
nats --context a consumer report jobs     # 各 topic 的积压和处理中任务
```

tenon 的 topic 对应同名 consumer，`.` 换成 `_`：`mail.send` → `mail_send`。暂停 topic 时任务照常发布和保存，worker 不领取新任务，tenon 队列面板也可以暂停和恢复：

```bash
nats --context a consumer pause jobs mail_send 1h
nats --context a consumer resume jobs mail_send
```

修改 `compose.yaml`，或者 `nats.conf` 中的 `server_name`、监听地址之后，执行 `sudo docker compose up -d --force-recreate nats`。

升级：磁盘快照 → 修改 `compose.yaml` 中的镜像版本 → `sudo docker compose up -d --wait`。重启期间任务暂停处理，发布不受影响。

## 监控

tenon 应用的指标由应用直接上报：`queue.relay.active` 所有实例之和为 0 时告警，另外关注 `queue.relay.lag` 和队列面板。

NATS 和主机按 [OpenObserve 的 NATS 监控文档](https://openobserve.ai/blog/how-to-monitor-nats/) 接入：在服务器上安装 OpenObserve 的 agent（systemd 服务 `otel-collector`），它默认采集主机指标、journald（其中有 NATS 的日志）和 `/var/log`，再让它抓取 `exporter` 给出的 NATS 指标。

在 OpenObserve 的 Data sources → Linux 中复制安装命令，形如：

```bash
curl -O https://raw.githubusercontent.com/openobserve/agents/main/linux/install.sh && chmod +x install.sh
sudo ./install.sh https://<OpenObserve 地址>/api/<组织>/ <令牌>
```

在 `/etc/otel-config.yaml` 的 `receivers` 中加一项：

```yaml
  prometheus/nats:
    config:
      scrape_configs:
        - job_name: nats
          scrape_interval: 30s
          static_configs:
            - targets: ["127.0.0.1:7777"]
```

再把 `service.pipelines.metrics` 的 `receivers` 改为 `[hostmetrics, prometheus/nats]`，然后检查并重启。重新运行 `install.sh`（例如升级 agent）会覆盖这个文件，之后要再加一次。

```bash
sudo /usr/local/bin/otelcol-contrib validate --config /etc/otel-config.yaml
sudo systemctl restart otel-collector
```

在 Dashboards → Import 中导入官方看板 [nats_dashboard.json](https://openobserve-prod-website.s3.us-west-2.amazonaws.com/assets/nats_dashboard_c317f090bb.json)。告警：

| 指标或日志 | 条件 |
|---|---|
| `jetstream_consumer_num_pending` | 持续增长（按 `account_name`、`consumer_name` 区分账号和 topic） |
| `jetstream_server_total_message_bytes` | 超过 `jetstream_server_max_storage` 的 80% |
| `gnatsd_healthz_status_value` | 不为 0，或停止上报 |
| `system.memory.utilization` | 超过 80%（定时消息占用内存） |
| `system.filesystem.utilization` | 数据盘超过 80% |
| journald 中 `SYSLOG_IDENTIFIER` 为 `nats` 的日志 | 出现 `[ERR]` |

## 迁移

把账号 `A` 迁到单独的 NATS。账号和密码不变，应用只改地址。

worker 停止期间发布照常进行，新任务留在 PostgreSQL 中，worker 连上新 NATS 后自动转发；NATS 中已有的任务、定时消息和 consumer 进度通过备份恢复迁移。任务暂停处理的时长等于 worker 停止的时长。

1. 按 [安装](#安装) 部署新服务器，`config/nats.conf` 的 `accounts` 只保留 `SYS` 和 `A`，`A` 的用户和密码与旧服务器相同。安全组放行应用服务器和旧服务器访问新服务器的 4222。
2. 在旧服务器上切换：

   ```bash
   nats context add a_new --server nats://<新地址>:4222 --user a --password <密码>
   # 平滑停止使用这个账号的全部 worker（SIGTERM），发布进程不停；
   # worker 与 HTTP 在同一进程（start）时，期间改为只运行 serve
   nats --context a backup account backup/a -f
   nats --context a_new backup restore account backup/a
   nats --context a_new stream info jobs | grep Messages      # 与旧服务器相同
   # 应用的 nats.url 改为新服务器，启动 worker，重启其他使用 NATS 的进程
   nats --context a_new consumer report jobs
   ```

3. 确认正常后立即按 [删除账号](#删除账号) 从旧服务器删掉 `A`，再 `sudo rm -rf backup/a`。不及时删除时，旧 stream 中的定时消息到时触发，误连旧服务器的 worker 会重复处理。

回退：删除之前，`nats.url` 改回旧服务器，重启 worker。

## 跨账号 RPC

账号 B、C 中的应用调用账号 A 中的 natsrpc 端点，以 A 的前缀 `a.v1` 为例。NATS 的导出、导入决定请求能否送到 A；natsrpc 的 token 和端点权限决定能否调用，调用方仍需 A 签发的 API token（`natsrpc.Token`）。

### 同一台 NATS：导出和导入

在 `config/nats.conf` 的 A 中导出一次，每个调用方导入，然后 `make reload`。A 中加：

```
    exports = [
      { service: "a.v1.>" }
    ]
```

B 中加：

```
    imports = [
      { service: { account: A, subject: "a.v1.>" } }
    ]
```

调用方用自己的连接调用，代码不变；未导出的 subject 返回 `natsrpc.ErrNoResponders`：

```go
out, err := natsrpc.Call[ArticleDTO](ctx, nc, "a.v1.articles.get", in, natsrpc.Token(token))
```

### 不同的 NATS：调用方直连 A

A 迁到单独的 NATS 后，导入失效。在 A 的 NATS 上，A 的 `users` 中为每个调用方加一个只能调用 RPC 的用户，然后 `make reload`：

```
      {
        user: rpc_b, password: "<密码>"
        permissions: {
          publish: "a.v1.>"
          subscribe: "_INBOX_b.>"
        }
      }
```

每个调用方的收件箱前缀专用且各不相同（C 用 `rpc_c` 和 `_INBOX_c`）。允许订阅 `_INBOX.>` 或共用前缀时，调用方能收到 A 账号内其他请求的回复。

调用方另建一条连接。`natsx.Connect` 不能设置收件箱前缀，这条连接直接用 nats.go 建立：

```go
nca, err := nats.Connect("nats://rpc_b:<密码>@<A 的地址>:4222",
    nats.CustomInboxPrefix("_INBOX_b"),
    nats.MaxReconnects(-1))
out, err := natsrpc.Call[ArticleDTO](ctx, nca, "a.v1.articles.get", in, natsrpc.Token(token))
```

该用户只能调用 `a.v1.*`；A 的其他 subject、默认 `_INBOX` 前缀、订阅 `_INBOX.>`、A 的 JetStream 均被拒绝。
