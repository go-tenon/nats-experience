# 04 接入 OpenObserve

参照 [OpenObserve 的 NATS 集成文档](https://openobserve.ai/docs/integration/message-brokers/nats/)：prometheus-nats-exporter 读取 NATS 的监控端口，OpenTelemetry Collector 采集它的指标，推送到 OpenObserve。tenon 应用的指标由应用直接上报。

命令都在 `/opt/nats` 中执行。

## 采集配置

`sudo vim otelcol.yaml`，填上 OpenObserve 的地址和令牌：

```yaml
receivers:
  prometheus:
    config:
      scrape_configs:
        - job_name: nats
          scrape_interval: 30s
          static_configs:
            - targets: ["exporter:7777"]

processors:
  batch:
    timeout: 5s

exporters:
  otlphttp/openobserve:
    endpoint: https://<OpenObserve 地址>/api/<组织>
    headers:
      Authorization: Basic <令牌>

service:
  pipelines:
    metrics:
      receivers: [prometheus]
      processors: [batch]
      exporters: [otlphttp/openobserve]
```

文件中有令牌，保存后只给 otelcol 容器的用户（10001）读取：

```bash
sudo chown 10001:10001 otelcol.yaml && sudo chmod 600 otelcol.yaml
```

`<令牌>` 是 OpenObserve 中 Data sources → Custom → Logs → OTEL Collector 给出的 `Basic` 后面的部分。

## 启动

用 `sudo vim compose.yaml` 在 `services` 中加上两个服务：

```yaml
  exporter:
    image: natsio/prometheus-nats-exporter:0.20.3
    command: ["-varz", "-connz", "-healthz", "-accstatz", "-jsz=all", "http://nats:8222"]
    restart: unless-stopped
    depends_on: [nats]

  otelcol:
    image: otel/opentelemetry-collector-contrib:0.165.0
    command: ["--config=/etc/otelcol/otelcol.yaml"]
    restart: unless-stopped
    volumes:
      - ./otelcol.yaml:/etc/otelcol/otelcol.yaml:ro
    depends_on: [exporter]
```

```bash
sudo docker compose up -d
sudo docker compose logs --tail 20 otelcol          # Everything is ready
```

指标按名称各成一个 stream。在 Dashboards → Import 中导入官方看板 [nats_dashboard.json](https://openobserve-prod-website.s3.us-west-2.amazonaws.com/assets/nats_dashboard_c317f090bb.json)。NATS 的日志不上报，用 `sudo docker compose logs nats` 查看。

修改 `otelcol.yaml` 后执行 `sudo docker compose up -d --force-recreate otelcol`：容器挂载的是单个文件，vim 保存后要重建容器才能读到新内容。

## 告警

| 指标 | 条件 |
|---|---|
| `jetstream_consumer_num_pending` | 持续增长（按 `account_name`、`consumer_name` 区分产品和 topic） |
| `jetstream_server_total_message_bytes` | 超过 `jetstream_server_max_storage` 的 80% |
| `gnatsd_healthz_status_value` | 不为 0，或停止上报 |
