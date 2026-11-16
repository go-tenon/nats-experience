# 05 接入 OpenObserve

参照 [OpenObserve 的 NATS 集成文档](https://openobserve.ai/docs/integration/message-brokers/nats/)。在 NATS 主机上运行：

| 组件 | 作用 |
|---|---|
| prometheus-nats-exporter | 读取 `127.0.0.1:8222`，指标在 `127.0.0.1:7777` |
| otelcol-contrib | 采集 exporter 的指标和 `/var/log/nats/nats-server.log`，以 OTLP 推送到 OpenObserve |

tenon 应用的指标（`queue.*`）由应用直接上报，不经过这里。

## 安装

```bash
curl -fLO https://github.com/nats-io/prometheus-nats-exporter/releases/download/v0.20.3/prometheus-nats-exporter-v0.20.3-linux-x86_64.deb
curl -fLO https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.165.0/otelcol-contrib_0.165.0_linux_amd64.deb
sudo apt install -y ./prometheus-nats-exporter-v0.20.3-linux-x86_64.deb ./otelcol-contrib_0.165.0_linux_amd64.deb
```

exporter：

```bash
sudo cp etc/systemd/system/prometheus-nats-exporter.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now prometheus-nats-exporter
curl -s 127.0.0.1:7777/metrics | grep -c '^jetstream_'
```

Collector：

```bash
sudo cp etc/otelcol-contrib/config.yaml.example /etc/otelcol-contrib/config.yaml
sudo vi /etc/otelcol-contrib/config.yaml        # endpoint、Authorization
sudo chown root:otelcol-contrib /etc/otelcol-contrib/config.yaml && sudo chmod 640 /etc/otelcol-contrib/config.yaml
sudo usermod -aG nats otelcol-contrib           # 读取 /var/log/nats
sudo systemctl restart otelcol-contrib
sudo journalctl -u otelcol-contrib -n 20 | grep -iE 'error|Everything is ready'
```

- `endpoint`：`https://<OpenObserve 地址>/api/<组织>`。
- `Authorization`：`Basic` 加上 OpenObserve 中 Data sources → Custom → Logs → OTEL Collector 给出的令牌（`printf '<邮箱>:<密码>' | base64 -w0` 的结果）。

指标每 30 秒采集一次，按指标名各成一个 metrics stream；日志进入 stream `nats`，从 Collector 启动后的新日志开始。

官方提供 NATS 看板：在 OpenObserve 的 Dashboards → Import 中导入 [nats_dashboard.json](https://openobserve-prod-website.s3.us-west-2.amazonaws.com/assets/nats_dashboard_c317f090bb.json)。

## 常用指标

| 指标 | 标签 | 含义 |
|---|---|---|
| `jetstream_consumer_num_pending` | `account_name`、`stream_name`、`consumer_name` | topic 的积压 |
| `jetstream_consumer_num_ack_pending` | 同上 | 处理中的任务 |
| `jetstream_consumer_num_redelivered` | 同上 | 重新投递的任务 |
| `jetstream_consumer_num_waiting` | 同上 | worker 的拉取请求；积压大于 0 且为 0 时没有 worker 在处理 |
| `jetstream_account_storage_used` | `account_name` | 账号的磁盘用量 |
| `jetstream_server_total_message_bytes` | | 全部账号的数据量 |
| `jetstream_server_max_storage` | | 磁盘上限（`max_file_store`） |
| `gnatsd_varz_connections` | | 连接数 |
| `gnatsd_varz_mem`、`gnatsd_varz_cpu` | | NATS 进程的内存和 CPU |
| `gnatsd_accstatz_current_connections` | `account_name` | 账号的连接数 |
| `gnatsd_healthz_status_value` | | 健康状态，0 为正常 |

`account_name` 是账号名（`account`、`account_id` 中可能是公钥）；`consumer_name` 是 tenon 的 topic 把 `.` 换成 `_`。

## 告警建议

- `jetstream_consumer_num_pending` 持续增长；
- `jetstream_server_total_message_bytes` 超过 `jetstream_server_max_storage` 的 80%；
- `gnatsd_healthz_status_value` 不为 0，或指标停止上报；
- 日志中出现 `[ERR]`。
