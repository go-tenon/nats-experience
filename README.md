# NATS 部署与运维

多产品共用的单机 NATS（JetStream），供 tenon 的队列、广播和实时频道使用。每个产品一个账号；产品流量变大后迁到单独的 NATS。

## 快速开始

```bash
sudo apt-get install -y docker.io docker-compose-v2 && sudo usermod -aG docker $USER   # 重新登录
git clone git@github.com:go-tenon/nats-experience.git ~/nats && cd ~/nats
cp nats.conf.example nats.conf && chmod 600 nats.conf     # 填入密码哈希，见 docs/02-accounts.md
docker compose up -d
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'
```

云平台安全组放行应用服务器访问 4222；开启磁盘自动快照。

应用配置：

```yaml
nats:
  url: ${NATS_URL}   # nats://product_a:<密码>@<NATS 地址>:4222
queue:
  max_defer: 10m
```

## 文件

| 文件 | 作用 |
|---|---|
| `compose.yaml` | NATS；`observe` profile 为 OpenObserve 采集组件 |
| `nats.conf.example` | NATS 配置模板，复制为 `nats.conf` |
| `otelcol.yaml` | OpenTelemetry Collector 配置 |
| `.env.example` | OpenObserve 地址和认证，复制为 `.env` |

`nats.conf`、`.env` 含密码哈希和认证信息，已在 `.gitignore` 中。

## 文档

- [01 部署](docs/01-deploy.md)
- [02 账号](docs/02-accounts.md)
- [03 运维](docs/03-operations.md)
- [04 迁移](docs/04-migration.md)
- [05 接入 OpenObserve](docs/05-openobserve.md)
