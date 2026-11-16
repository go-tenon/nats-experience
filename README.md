# NATS 部署与运维

多产品共用的单机 NATS（JetStream），供 tenon 的队列、广播和实时频道使用。以 systemd 服务运行；认证用 JWT + NKey（`.creds`），每个产品一个账号；产品流量变大后迁到单独的 NATS。

## 快速开始

Ubuntu x86_64，以管理员用户执行：

```bash
curl -fLO https://github.com/nats-io/nats-server/releases/download/v2.15.3/nats-server-v2.15.3-amd64.deb
curl -fLO https://github.com/nats-io/nsc/releases/download/v2.15.0/nsc-v2.15.0-amd64.deb
curl -fLO https://github.com/nats-io/natscli/releases/download/v0.5.1/nats-0.5.1-amd64.deb
sudo apt install -y ./nats-server-v2.15.3-amd64.deb ./nsc-v2.15.0-amd64.deb ./nats-0.5.1-amd64.deb
```

其余步骤见 [01 部署](docs/01-deploy.md)：创建用户和目录、运营者、配置、systemd 服务、添加产品。

应用配置：

```yaml
nats:
  url: nats://<NATS 地址>:4222
  credentials: /etc/app/nats.creds
queue:
  max_defer: 10m
```

## 文件

按服务器上的路径存放：

| 文件 | 目标 |
|---|---|
| `etc/nats/nats.conf.example` | `/etc/nats/nats.conf` |
| `etc/systemd/system/nats-server.service` | `/etc/systemd/system/` |
| `etc/systemd/system/prometheus-nats-exporter.service` | `/etc/systemd/system/`（接入 OpenObserve 时） |
| `etc/otelcol-contrib/config.yaml.example` | `/etc/otelcol-contrib/config.yaml`（接入 OpenObserve 时） |

## 文档

- [01 部署](docs/01-deploy.md)
- [02 账号](docs/02-accounts.md)
- [03 运维](docs/03-operations.md)
- [04 迁移](docs/04-migration.md)
- [05 接入 OpenObserve](docs/05-openobserve.md)
- [06 跨账号 RPC](docs/06-cross-account-rpc.md)
