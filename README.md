# NATS 部署与运维

多产品共用的单机 NATS（JetStream），供 tenon 的队列、广播和实时频道使用。认证用 JWT + NKey（`.creds` 凭据文件），每个产品一个账号；产品流量变大后迁到单独的 NATS。

## 快速开始

```bash
sudo apt-get install -y docker.io docker-compose-v2 && sudo usermod -aG docker $USER   # 重新登录
git clone git@github.com:go-tenon/nats-experience.git ~/nats && cd ~/nats
nb() { docker run --rm -i --network host --user "$(id -u):$(id -g)" -v ~/nats/admin:/nsc natsio/nats-box:latest "$@"; }

# 运营者和服务器配置
mkdir -m 700 admin
nb nsc add operator --name tenon --generate-signing-key --sys
nb nsc edit operator --require-signing-keys --account-jwt-server-url nats://127.0.0.1:4222
nb nsc generate config --nats-resolver --sys-account SYS > resolver.conf
sed -i "s|dir: './jwt'|dir: '/data/jwt'|; s|allow_delete: false|allow_delete: true|" resolver.conf
cp nats.conf.example nats.conf            # 修改 server_name
docker compose up -d

# 产品
nb nsc add account --name PRODUCT_A
nb nsc edit account --name PRODUCT_A --sk generate --js-disk-storage -1 --js-mem-storage -1 --js-streams -1 --js-consumer -1
nb nsc add user --account PRODUCT_A --name app
nb nsc push --account PRODUCT_A
# 凭据：admin/nkeys/creds/tenon/PRODUCT_A/app.creds
```

云平台安全组放行应用服务器访问 4222；开启磁盘自动快照；备份 `admin/`。

应用配置：

```yaml
nats:
  url: nats://<NATS 地址>:4222
  credentials: /etc/app/nats.creds
queue:
  max_defer: 10m
```

## 文件

| 文件 | 作用 |
|---|---|
| `compose.yaml` | NATS；`observe` profile 为 OpenObserve 采集组件 |
| `nats.conf.example` | NATS 配置模板 |
| `otelcol.yaml` | OpenTelemetry Collector 配置 |
| `.env.example` | OpenObserve 地址和认证 |

部署时产生的 `admin/`（签名私钥、凭据）、`resolver.conf`、`nats.conf`、`.env` 不提交。

## 文档

- [01 部署](docs/01-deploy.md)
- [02 账号](docs/02-accounts.md)
- [03 运维](docs/03-operations.md)
- [04 迁移](docs/04-migration.md)
- [05 接入 OpenObserve](docs/05-openobserve.md)
