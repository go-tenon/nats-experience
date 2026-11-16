# 05 接入 OpenObserve

NATS 不主动推送数据。`compose.yaml` 的 `observe` profile 在 NATS 主机上运行两个组件：

| 组件 | 作用 |
|---|---|
| `nats-exporter` | prometheus-nats-exporter，读取 `127.0.0.1:8222`，指标在 `127.0.0.1:7777` |
| `otelcol` | OpenTelemetry Collector，采集上述指标和本机所有容器的日志，以 OTLP 推送到 OpenObserve |

tenon 应用的指标（`queue.*`）由应用直接上报，不经过这里。

## 启用

```bash
cp .env.example .env && chmod 600 .env
vi .env                                   # OO_URL、OO_ORG、OO_AUTH、OO_STREAM
docker compose --profile observe up -d
docker compose logs otelcol | grep -iE 'error|Everything is ready'
```

`OO_AUTH` 的值：`Basic ` 加上 `printf '<邮箱>:<密码>' | base64 -w0` 的结果。

指标每 30 秒采集一次，在 OpenObserve 中按指标名各成一个 metrics stream；日志进入 `OO_STREAM`（默认 `nats`）。

停用：`docker compose --profile observe rm -sf nats-exporter otelcol`。

## 常用指标

| 指标 | 标签 | 含义 |
|---|---|---|
| `jetstream_consumer_num_pending` | `account`、`stream_name`、`consumer_name` | topic 的积压 |
| `jetstream_consumer_num_ack_pending` | 同上 | 处理中的任务 |
| `jetstream_consumer_num_redelivered` | 同上 | 重新投递的任务 |
| `jetstream_consumer_num_waiting` | 同上 | worker 的拉取请求；积压大于 0 且为 0 时没有 worker 在处理 |
| `jetstream_account_storage_used` | `account` | 账号的磁盘用量 |
| `jetstream_account_max_storage` | `account` | 账号的磁盘配额 |
| `jetstream_server_total_message_bytes` | | 全部 stream 的数据量 |
| `gnatsd_varz_connections` | | 连接数 |
| `gnatsd_varz_mem`、`gnatsd_varz_cpu` | | NATS 进程的内存和 CPU |
| `gnatsd_accstatz_current_connections` | `account` | 账号的连接数 |
| `gnatsd_healthz_status_value` | | 健康状态，0 为正常 |

`consumer_name` 是 tenon 的 topic 把 `.` 换成 `_`。

## 告警建议

- `jetstream_consumer_num_pending` 持续增长；
- `jetstream_account_storage_used` 超过 `jetstream_account_max_storage` 的 80%；
- `gnatsd_healthz_status_value` 不为 0，或指标停止上报；
- 日志中出现 `[ERR]`。
