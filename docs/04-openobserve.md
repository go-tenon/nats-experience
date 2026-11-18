# 04 接入 OpenObserve

在服务器上安装 OpenObserve 的 agent，参照 [OpenObserve 的 NATS 监控文档](https://openobserve.ai/blog/how-to-monitor-nats/)。agent 是装成 systemd 服务 `otel-collector` 的 OpenTelemetry Collector，默认采集：

- 主机指标：CPU、内存、磁盘、文件系统、网络；
- systemd 日志（journald），其中有 NATS 的日志（`compose.yaml` 中 nats 的日志驱动是 `journald`）；
- `/var/log` 下的日志文件。

再让它抓取 `compose.yaml` 中 `exporter`（prometheus-nats-exporter）给出的 NATS 指标。tenon 应用的指标由应用直接上报。

## 安装 agent

在 OpenObserve 的 Data sources → Linux 中复制安装命令，形如：

```bash
curl -O https://raw.githubusercontent.com/openobserve/agents/main/linux/install.sh && chmod +x install.sh
sudo ./install.sh https://<OpenObserve 地址>/api/<组织>/ <令牌>
```

```bash
systemctl status otel-collector
journalctl -u otel-collector -n 20
```

## 抓取 NATS 指标

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

再把 `service.pipelines.metrics` 的 `receivers` 改为 `[hostmetrics, prometheus/nats]`，然后检查并重启：

```bash
sudo /usr/local/bin/otelcol-contrib validate --config /etc/otel-config.yaml
sudo systemctl restart otel-collector
```

重新运行 `install.sh`（例如升级 agent）会覆盖 `/etc/otel-config.yaml`，之后再按这一节加一次。

## 看板

NATS 的指标按名称各成一个 stream。在 Dashboards → Import 中导入官方看板 [nats_dashboard.json](https://openobserve-prod-website.s3.us-west-2.amazonaws.com/assets/nats_dashboard_c317f090bb.json)。

## 告警

| 指标或日志 | 条件 |
|---|---|
| `jetstream_consumer_num_pending` | 持续增长（按 `account_name`、`consumer_name` 区分产品和 topic） |
| `jetstream_server_total_message_bytes` | 超过 `jetstream_server_max_storage` 的 80% |
| `gnatsd_healthz_status_value` | 不为 0，或停止上报 |
| `system.memory.utilization` | 超过 80%（定时消息占用内存，见 [机器](../README.md#机器)） |
| `system.filesystem.utilization` | 数据盘超过 80% |
| journald 中 `SYSLOG_IDENTIFIER` 为 `nats` 的日志 | 出现 `[ERR]` |
