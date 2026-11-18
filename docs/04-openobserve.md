# 04 接入 OpenObserve

参照 [OpenObserve 的 NATS 集成文档](https://openobserve.ai/docs/integration/message-brokers/nats/)：`exporter`（prometheus-nats-exporter）读取 NATS 的监控端口，`otelcol`（OpenTelemetry Collector）采集它的指标，推送到 OpenObserve。tenon 应用的指标由应用直接上报。

## 启动

```bash
cd /opt/nats
cp .env.example .env
vi .env                    # OPENOBSERVE_ENDPOINT、OPENOBSERVE_TOKEN
chmod 600 .env
sudo docker compose --profile monitoring up -d
sudo docker compose --profile monitoring logs --tail 20 otelcol
```

- `OPENOBSERVE_ENDPOINT`：`https://<OpenObserve 地址>/api/<组织>`。
- `OPENOBSERVE_TOKEN`：OpenObserve 中 Data sources → Custom → Logs → OTEL Collector 给出的令牌（`Basic` 后面的部分）。

之后执行 `docker compose up -d` 时都加上 `--profile monitoring`，否则这两个容器不会随配置更新。

指标按名称各成一个 stream。在 Dashboards → Import 中导入官方看板 [nats_dashboard.json](https://openobserve-prod-website.s3.us-west-2.amazonaws.com/assets/nats_dashboard_c317f090bb.json)。NATS 的日志不上报，用 `docker compose logs nats` 查看。

## 告警

| 指标 | 条件 |
|---|---|
| `jetstream_consumer_num_pending` | 持续增长（按 `account_name`、`consumer_name` 区分产品和 topic） |
| `jetstream_server_total_message_bytes` | 超过 `jetstream_server_max_storage` 的 80% |
| `gnatsd_healthz_status_value` | 不为 0，或停止上报 |
