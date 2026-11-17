# 05 接入 OpenObserve

参照 [OpenObserve 的 NATS 集成文档](https://openobserve.ai/docs/integration/message-brokers/nats/)：prometheus-nats-exporter 读取 `127.0.0.1:8222`，otelcol-contrib 采集 exporter 的指标和 NATS 日志文件，推送到 OpenObserve。tenon 应用的指标由应用直接上报。

## 安装

```bash
curl -fLO https://github.com/nats-io/prometheus-nats-exporter/releases/download/v0.20.3/prometheus-nats-exporter-v0.20.3-linux-x86_64.deb
curl -fLO https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.165.0/otelcol-contrib_0.165.0_linux_amd64.deb
sudo apt install -y ./prometheus-nats-exporter-v0.20.3-linux-x86_64.deb ./otelcol-contrib_0.165.0_linux_amd64.deb

sudo cp etc/systemd/system/prometheus-nats-exporter.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now prometheus-nats-exporter

sudo cp etc/otelcol-contrib/config.yaml.example /etc/otelcol-contrib/config.yaml
sudo vi /etc/otelcol-contrib/config.yaml        # endpoint、Authorization
sudo chown root:otelcol-contrib /etc/otelcol-contrib/config.yaml && sudo chmod 640 /etc/otelcol-contrib/config.yaml
sudo usermod -aG nats otelcol-contrib           # 读取 /var/log/nats
sudo systemctl restart otelcol-contrib
```

- `endpoint`：`https://<OpenObserve 地址>/api/<组织>`。
- `Authorization`：`Basic` 加 OpenObserve 中 Data sources → Custom → Logs → OTEL Collector 给出的令牌。

日志进入 stream `nats`，指标按名称各成一个 stream。在 Dashboards → Import 中导入官方看板 [nats_dashboard.json](https://openobserve-prod-website.s3.us-west-2.amazonaws.com/assets/nats_dashboard_c317f090bb.json)。

## 告警

| 指标 | 条件 |
|---|---|
| `jetstream_consumer_num_pending` | 持续增长（按 `account_name`、`consumer_name` 区分产品和 topic） |
| `jetstream_server_total_message_bytes` | 超过 `jetstream_server_max_storage` 的 80% |
| `gnatsd_healthz_status_value` | 不为 0，或停止上报 |
| 日志 | 出现 `[ERR]` |
