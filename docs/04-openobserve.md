# 04 接入 OpenObserve

参照 [OpenObserve 的 NATS 集成文档](https://openobserve.ai/docs/integration/message-brokers/nats/)：`compose.yaml` 中的 `exporter`（prometheus-nats-exporter）读取 NATS 的监控端口，`otelcol`（OpenTelemetry Collector）采集它的指标，推送到 OpenObserve。tenon 应用的指标由应用直接上报。

## 配置

otelcol 的采集配置写在 `compose.yaml` 末尾的 `configs.otelcol` 中，要填两处：

- `endpoint`：`https://<OpenObserve 地址>/api/<组织>`。
- `Authorization`：`Basic` 加 OpenObserve 中 Data sources → Custom → Logs → OTEL Collector 给出的令牌。

修改后重建 otelcol：只改 `configs` 的内容时，`docker compose up -d` 不会重建容器，要加 `--force-recreate`。

```bash
cd /opt/nats
sudo docker compose up -d --force-recreate otelcol
sudo docker compose logs --tail 20 otelcol          # Everything is ready
```

不接入 OpenObserve 时，从 `compose.yaml` 中删掉 `exporter`、`otelcol` 两个服务和末尾的 `configs`。

## 看板

指标按名称各成一个 stream。在 Dashboards → Import 中导入官方看板 [nats_dashboard.json](https://openobserve-prod-website.s3.us-west-2.amazonaws.com/assets/nats_dashboard_c317f090bb.json)。NATS 的日志不上报，用 `sudo docker compose logs nats` 查看。

## 告警

| 指标 | 条件 |
|---|---|
| `jetstream_consumer_num_pending` | 持续增长（按 `account_name`、`consumer_name` 区分产品和 topic） |
| `jetstream_server_total_message_bytes` | 超过 `jetstream_server_max_storage` 的 80% |
| `gnatsd_healthz_status_value` | 不为 0，或停止上报 |
