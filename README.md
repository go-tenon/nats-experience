# NATS 部署与运维

多个产品共用的单机 NATS（JetStream）：部署、账号、日常运维和迁移。给 tenon 应用的队列、广播和实时频道使用。

前期多个产品共用一台 NATS，每个产品一个账号；某个产品流量起来后，再迁到单独的 NATS。

## 快速开始

在一台 Ubuntu 云主机上（与应用服务器同一个 VPC）：

```bash
bash scripts/install-docker.sh                       # 之后重新登录一次
ALLOW_CIDR=<VPC 网段> bash scripts/install-nats.sh product_a product_b
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'
sudo cat /etc/nats/product_a.password                # 产品 A 的密码
```

应用的配置：

```yaml
nats:
  url: ${NATS_URL}   # nats://product_a:<密码>@<内网 IP>:4222
queue:
  max_defer: 10m
```

然后在控制台把安全组设为只放行应用服务器访问 4222，并开启磁盘自动快照。

## 文档

- [01 部署](docs/01-deploy.md)：机器规格、何时需要集群、安装、网络安全、配置说明、应用的配置
- [02 多产品账号](docs/02-accounts.md)：账号隔离了什么、新增和删除产品、配额、用量
- [03 日常运维](docs/03-operations.md)：状态、nats 命令行、暂停 topic、改配置、换密码、升级、备份、监控
- [04 迁移](docs/04-migration.md)：把一个产品搬到单独的 NATS

## 文件

```
scripts/install-docker.sh   在 Ubuntu 上安装 Docker
scripts/install-nats.sh     部署或更新 NATS：账号、密码、配置、容器、防火墙；可以重复运行
config/nats.conf.example    配置模板，install-nats.sh 按这个结构生成
```
