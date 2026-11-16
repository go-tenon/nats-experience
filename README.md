# NATS 部署与运维

多产品共用的单机 NATS（JetStream），供 tenon 的队列、广播和实时频道使用。每个产品一个账号；产品流量变大后迁到单独的 NATS。

## 快速开始

Ubuntu 云主机，与应用服务器同一个 VPC：

```bash
bash scripts/install-docker.sh            # 完成后重新登录
ALLOW_CIDR=<VPC 网段> bash scripts/install-nats.sh product_a product_b
sudo cat /etc/nats/product_a.password     # product_a 的密码
```

应用配置：

```yaml
nats:
  url: ${NATS_URL}   # nats://product_a:<密码>@<内网 IP>:4222
queue:
  max_defer: 10m
```

上线前：安全组只放行应用服务器访问 4222；开启磁盘自动快照。

## 文档

- [01 部署](docs/01-deploy.md)
- [02 账号](docs/02-accounts.md)
- [03 运维](docs/03-operations.md)
- [04 迁移](docs/04-migration.md)

## 文件

| 文件 | 作用 |
|---|---|
| `scripts/install-docker.sh` | 安装 Docker |
| `scripts/install-nats.sh` | 部署或更新 NATS：账号、密码、配置、容器、防火墙。可重复运行 |
| `config/nats.conf.example` | 配置模板 |
