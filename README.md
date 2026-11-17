# NATS 部署与运维

多产品共用的单机 NATS（JetStream），供 tenon 的队列、广播、实时频道和 RPC 使用。systemd 运行；JWT + NKey 认证，每个产品一个账号；产品流量变大后迁到单独的 NATS。

## 文档

- [01 部署](docs/01-deploy.md)
- [02 账号](docs/02-accounts.md)
- [03 运维](docs/03-operations.md)
- [04 迁移](docs/04-migration.md)
- [05 接入 OpenObserve](docs/05-openobserve.md)
- [06 跨账号 RPC](docs/06-cross-account-rpc.md)

## 文件

按服务器上的路径存放：

| 文件 | 目标 |
|---|---|
| `etc/nats/nats.conf.example` | `/etc/nats/nats.conf` |
| `etc/systemd/system/nats-server.service` | `/etc/systemd/system/` |
| `etc/systemd/system/prometheus-nats-exporter.service` | `/etc/systemd/system/`（接入 OpenObserve 时） |
| `etc/otelcol-contrib/config.yaml.example` | `/etc/otelcol-contrib/config.yaml`（接入 OpenObserve 时） |
