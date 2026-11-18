# NATS 部署与运维

多项目共用的单机 NATS（JetStream），供 tenon 的队列、广播、实时频道和 RPC 使用。用 systemd 运行，只有 `/etc/nats/nats.conf` 和两个 systemd unit 三个文件；每个项目一个账号（account），账号和密码写在 `nats.conf` 中，增删账号热加载生效；某个项目流量变大后迁到单独的 NATS。

## 机器

Ubuntu x86_64。2 核 2G 起，ESSD 并开启每天自动快照（JetStream 数据在 `/var/lib/nats`，在数据盘上时挂载到这个目录），与应用服务器在同一 VPC，连接走内网明文传输。

- CPU：2 核 2G 上 NATS 每秒处理上万条消息，CPU 不到 1 核。
- 内存：随积压和定时消息增长。消息内容存在磁盘上，内存是每条定时消息的索引和到期计时，每条约 0.75KB。tenon 的延迟任务和重试全部是定时消息，一直留在 NATS 中直到执行，100 万条约 750MB；应用会大量安排远期任务时，按条数预留内存。
- 磁盘：按最大积压计算，1KB 的任务每 100 万条约 1GB。

故障影响：

| 情况 | 影响 |
|---|---|
| 进程崩溃、重启、升级 | 任务保留：处理暂停，发布照常；广播、实时频道和 RPC 中断，客户端自动重连 |
| 断电、宕机 | 丢最近约 1 秒写入的任务 |
| 磁盘损坏 | 丢 NATS 中未处理的任务，包括全部延迟任务和重试；只能从磁盘快照恢复到快照时的状态 |

## 安装

参考 [OpenObserve 的 NATS 接入文档](https://openobserve.ai/docs/integration/message-brokers/nats/)，用官方二进制安装到 `/usr/local/bin`，配置在 `/etc/nats/nats.conf`，由 systemd 运行，日志写入 journald。

### 程序

下载 NATS、`nats` 命令行和指标导出器，创建运行 NATS 的用户和目录：

```bash
sudo apt-get update && sudo apt-get install -y unzip
cd /tmp
curl -fsSLO https://github.com/nats-io/nats-server/releases/download/v2.15.3/nats-server-v2.15.3-linux-amd64.tar.gz
curl -fsSLO https://github.com/nats-io/natscli/releases/download/v0.5.2/nats-0.5.2-linux-amd64.zip
curl -fsSLO https://github.com/nats-io/prometheus-nats-exporter/releases/download/v0.20.3/prometheus-nats-exporter-v0.20.3-linux-x86_64.tar.gz

sudo tar xzf nats-server-v2.15.3-linux-amd64.tar.gz -C /usr/local/bin --strip-components=1 nats-server-v2.15.3-linux-amd64/nats-server
sudo unzip -jo nats-0.5.2-linux-amd64.zip nats-0.5.2-linux-amd64/nats -d /usr/local/bin
sudo tar xzf prometheus-nats-exporter-v0.20.3-linux-x86_64.tar.gz -C /usr/local/bin prometheus-nats-exporter
nats-server --version && nats --version && prometheus-nats-exporter -version

sudo useradd --system --no-create-home --shell /usr/sbin/nologin nats
sudo install -d -o root -g nats -m 750 /etc/nats
sudo install -d -o nats -g nats -m 750 /var/lib/nats
```

### /etc/nats/nats.conf

把账号名 `A`、用户名 `a` 换成实际的名字，每个 `<密码>` 换成 `openssl rand -hex 24` 生成的不同的随机串：

```
# 修改后执行 sudo systemctl reload nats-server。修改 server_name、listen、http 或 jetstream 段
# （调大 max_file_store、max_memory_store 除外）后，执行 sudo systemctl restart nats-server
server_name: nats-1

# 客户端，访问来源由云平台安全组控制
listen: 0.0.0.0:4222
# 监控，只给本机
http: 127.0.0.1:8222

jetstream {
  store_dir: /var/lib/nats
  # 每秒同步磁盘，吞吐下降 5–10%
  sync_interval: "1s"
  # 所有账号共用的磁盘上限，小于数据盘的可用空间
  max_file_store: 30GB
  # 实时频道的在线成员使用内存存储
  max_memory_store: 256MB
}

# 用户名在所有账号中唯一
system_account: SYS

accounts {
  # 系统账号：查看服务器状态
  SYS {
    users = [
      { user: admin, password: "<密码>" }
    ]
  }

  A {
    # 开启 JetStream，用量计入共用的上限
    jetstream: enabled
    users = [
      { user: a, password: "<密码>" }
    ]
  }
}
```

只让 `nats` 用户读取：

```bash
sudo chown root:nats /etc/nats/nats.conf && sudo chmod 640 /etc/nats/nats.conf
```

文件中有全部账号的密码，另外保存到离线或加密存储。

### /etc/systemd/system/nats-server.service

```ini
[Unit]
Description=NATS Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=nats
Group=nats
ExecStart=/usr/local/bin/nats-server -c /etc/nats/nats.conf
# reload：先检查配置语法，通过后发送 HUP 热加载，已有连接保持
ExecReload=/usr/local/bin/nats-server -t -c /etc/nats/nats.conf
ExecReload=/bin/kill -s HUP $MAINPID
KillSignal=SIGINT
TimeoutStopSec=30
Restart=always
RestartSec=2
LimitNOFILE=65536
# 内存的 60–80%
Environment=GOMEMLIMIT=1200MiB
# 日志写入 journald，由主机上的 OpenObserve agent 采集
SyslogIdentifier=nats

NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/nats

[Install]
WantedBy=multi-user.target
```

### /etc/systemd/system/prometheus-nats-exporter.service

把 NATS 的指标转成 Prometheus 格式，只监听本机，由主机上的 OpenObserve agent 抓取：

```ini
[Unit]
Description=Prometheus NATS Exporter
After=nats-server.service

[Service]
Type=simple
DynamicUser=true
ExecStart=/usr/local/bin/prometheus-nats-exporter -addr 127.0.0.1 -port 7777 -varz -connz_detailed -healthz -accstatz -jsz=all http://127.0.0.1:8222
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### 启动

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now nats-server prometheus-nats-exporter
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'     # {"status":"ok"}
```

修改 `nats.conf` 后热加载：

```bash
sudo systemctl reload nats-server && sleep 1
# 成功时有 Reloaded server configuration；出现 not supported 时 sudo systemctl restart nats-server
journalctl -u nats-server -n 5 --no-pager -o cat
```

语法检查不通过时 `reload` 报错（Job for nats-server.service failed），NATS 保持原配置运行，原因（Parse error）在 `journalctl -u nats-server` 中。

## 应用配置

```yaml
nats:
  url: nats://a:<密码>@<NATS 地址>:4222
```

下文假设 `queue.stream` 是默认的 `jobs`。`jobs.>` 只给队列用。`jobs` stream 的 `max_age` 保持为 0，容量上限配合 `discard: new`，延迟任务才能一直保留到执行；worker 启动时检查这两项。

## 命令行

`nats` 是 NATS 官方的命令行工具，用来查看和操作 NATS：看某个账号的 stream 和积压、暂停 topic、迁移时备份和恢复，以及查看整台服务器的状态。它装在 NATS 服务器上，用登录服务器的普通用户执行：

1. 保存登录信息。命令行和应用一样，要用 `/etc/nats/nats.conf` 中的某个用户连接 NATS，并且只能看到这个用户所属账号里的东西：用账号 `A` 的用户 `a` 连接，只能操作 `A` 的 stream 和 consumer；查看整台服务器要用系统账号 `SYS` 的用户 `admin`。把每个要用的用户保存一次，起个名字，这叫 context，存在 `~/.config/nats/` 中（含密码）：

   ```bash
   # 名字 sys：系统账号 SYS 的用户 admin
   nats context add sys --server nats://127.0.0.1:4222 --user admin --password <admin 的密码>
   # 名字 a：账号 A 的用户 a
   nats context add a --server nats://127.0.0.1:4222 --user a --password <a 的密码>
   ```

2. 操作时用 `--context` 选择以哪个用户的身份执行：

   ```bash
   nats --context sys server report accounts     # 各账号的连接数和流量
   nats --context sys server report jetstream    # 各账号的 stream 数、消息数和存储
   nats --context a stream ls                    # 账号 A 中的 stream
   ```

   下文的 `nats` 命令都按这个方式指定账号。

`/etc/nats/nats.conf` 中删掉一个账号后，用 `nats context rm <名字> -f` 删掉对应的 context。

## 运维

状态：

```bash
systemctl status nats-server prometheus-nats-exporter    # 都是 active (running)
nats --context a consumer report jobs     # 各 topic 的积压和处理中任务
```

tenon 的 topic 对应同名 consumer，`.` 换成 `_`：`mail.send` → `mail_send`。暂停 topic 时任务照常发布和保存，worker 不领取新任务，tenon 队列面板也可以暂停和恢复：

```bash
nats --context a consumer pause jobs mail_send 1h
nats --context a consumer resume jobs mail_send
```

日志：`journalctl -u nats-server -f`。

修改 unit 文件后执行 `sudo systemctl daemon-reload`，再 `sudo systemctl restart` 对应的服务；修改了 `nats.conf` 开头注释中列出的设置后执行 `sudo systemctl restart nats-server`。

升级：磁盘快照 → 按 [程序](#程序) 下载新版本覆盖 `/usr/local/bin/nats-server` → `sudo systemctl restart nats-server` → 检查 healthz。

## 监控

tenon 的队列指标由应用直接上报。接入应用指标后，可按项目对 `queue.relay.active` 的实例总和持续为 0 设置告警，并关注 `queue.relay.lag` 和队列面板。可为指标停止上报另设告警。

NATS 和主机按 [OpenObserve 的 NATS 接入文档](https://openobserve.ai/docs/integration/message-brokers/nats/) 接入：在服务器上安装 OpenObserve 的 agent（systemd 服务 `otel-collector`），它默认采集主机指标、journald（其中有 NATS 的日志）和 `/var/log`，再让它抓取 `prometheus-nats-exporter`（`127.0.0.1:7777`）给出的 NATS 指标。

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

在 Dashboards → Import 中导入官方看板 [nats_dashboard.json](https://openobserve-prod-website.s3.us-west-2.amazonaws.com/assets/nats_dashboard_c317f090bb.json)。

验证 Idle connections 面板时，可在一个终端运行 `nats --context a sub monitoring.test.idle` 保持连接，再在另一个终端检查 exporter：

```bash
curl -fsS http://127.0.0.1:7777/metrics | grep '^gnatsd_connz_idle{'
```

查到样本后，等 agent 下一轮抓取，再刷新看板。验证结束后按 `Ctrl+C` 断开测试订阅。

### 告警

自部署 OpenObserve 的 `default` 文件夹中已启用以下两条 NATS 告警。两条规则均选择指标流 `gnatsd_healthz_status_value`，每 1 分钟评估一次，查询周期为 1 分钟，至少一条时间序列的查询结果 ≥ 1 时满足触发条件。部署到其他主机时，按实际采集标签修改 `host_name` 和 `service_name`。

| 告警规则 | 触发条件 |
|---|---|
| `nats_health_check_failed` | NATS 健康指标为 `0`，持续 1 分钟。健康时指标为 `1`。 |
| `nats_metrics_interrupted` | 最近 1 分钟有抓取样本且全部失败（`up=0`），或连续 3 分钟没有收到 NATS 健康指标。满足任一条件即告警，无额外等待。 |

`nats_health_check_failed` 的 PromQL

```promql
1 - min by (host_name) (gnatsd_healthz_status_value{host_name="nats"})
```

`nats_metrics_interrupted` 的 PromQL

```promql
(1 + (max by (host_name) (max_over_time(up{host_name="nats",service_name="nats"}[1m])) == 0))
or on (host_name)
absent_over_time(gnatsd_healthz_status_value{host_name="nats"}[3m])
```

两条规则共用 `telegram-host-warning`，重复通知的静默时间为 15 分钟。条件解除后继续保持告警状态 2 分钟，恢复通知发往 `telegram-host-recovery`。

CPU、内存和磁盘沿用通用主机告警。CPU 使用率 ≥ 80%，内存使用率（`state="used"`）≥ 75%。当前 `/var/lib/nats` 位于根分区 `/`，磁盘沿用通用的 **70%** 告警，即 `system_filesystem_utilization{mountpoint="/"}` 换算成百分比后达到或超过 70%。如果日后把 JetStream 数据移到独立挂载点，需给该挂载点配置磁盘告警。

以下是业务需要时可补充的告警，配置前先确认对应指标或日志已经入库。

| 监控项 | 条件 |
|---|---|
| JetStream 消费积压 | 对 `jetstream_consumer_num_pending` 按 `account_name`、`stream_name`、`consumer_name` 区分，按业务可接受的积压量和持续时间设置阈值。 |
| JetStream 存储配额 | 按 `host_name`、`server_id` 匹配，用 `gnatsd_varz_jetstream_stats_storage` 除以 `gnatsd_varz_jetstream_config_max_storage`。仅在配额大于 0 时计算，可在使用率达到 80% 时预警。 |
| NATS 错误日志 | 在 `journald` 中筛选 NATS 主机，按 `body__systemd_unit` 为 `nats-server.service` 或 `body_syslog_identifier` 为 `nats` 识别服务，再匹配 `body_message` 中的 `[ERR]`。 |

## 迁移

把账号 `A` 迁到单独的 NATS。账号和密码不变，应用只改地址。

worker 停止期间发布照常进行，新任务留在 PostgreSQL 中，worker 连上新 NATS 后自动转发；NATS 中已有的任务、定时消息和 consumer 进度通过备份恢复迁移。任务暂停处理的时长等于 worker 停止的时长。

1. 按 [安装](#安装) 部署新服务器，`/etc/nats/nats.conf` 的 `accounts` 只保留 `SYS` 和 `A`，`A` 的用户和密码与旧服务器相同。安全组放行应用服务器和旧服务器访问新服务器的 4222。
2. 在旧服务器上切换：

   ```bash
   nats context add a_new --server nats://<新地址>:4222 --user a --password <密码>
   # 平滑停止使用这个账号的全部 worker（SIGTERM），发布进程不停；
   # worker 与 HTTP 在同一进程（start）时，期间改为只运行 serve 和 schedule
   nats --context a stream info jobs | grep Messages
   nats --context a backup account ~/nats-backup/a -f
   nats --context a_new backup restore account ~/nats-backup/a
   nats --context a_new stream info jobs | grep Messages  # 与上面旧服务器的数量相同
   # 应用的 nats.url 改为新服务器，启动 worker，重启其他使用 NATS 的进程
   nats --context a_new consumer report jobs
   ```

3. 确认正常后立即清理旧服务器：`nats --context a stream rm jobs -f`；从 `/etc/nats/nats.conf` 中删掉 `A`，`sudo systemctl reload nats-server`；`nats context rm a -f`；`sudo rm -rf /var/lib/nats/jetstream/A ~/nats-backup/a`。不及时清理时，旧 stream 中的定时消息到时触发，误连旧服务器的 worker 会重复处理。

回退：删除之前，`nats.url` 改回旧服务器，重启 worker。

## 跨账号 RPC

账号 B、C 中的应用调用账号 A 中的 natsrpc 端点时，直接连到 A 的账号。以 A 的前缀 `a.v1` 为例。

在 `/etc/nats/nats.conf` 的 A 中为每个调用方加一个用户，然后 `sudo systemctl reload nats-server`：

```
      { user: rpc_b, password: "<密码>" }
```

调用方另建一条连到 A 的连接，调用时带上 A 签发的 API token：

```go
ncA, err := natsx.Connect(natsx.Config{URL: "nats://rpc_b:<密码>@<NATS 地址>:4222"}, "b", logger)
out, err := natsrpc.Call[ArticleDTO](ctx, ncA, "a.v1.articles.get", in, natsrpc.Token(token))
```

这个用户能访问 A 账号中的全部 subject 和 stream，按团队约定只用它调用 RPC。A 迁到单独的 NATS 后，调用方只改连接地址。
