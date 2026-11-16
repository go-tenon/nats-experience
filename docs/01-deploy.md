# 01 部署

## 机器

| 项目 | 要求 |
|---|---|
| 规格 | 2 核 2G 起；对延迟稳定性有要求时用独享型实例 |
| 磁盘 | ESSD，开启自动快照。容量按最大积压计算：1KB 任务每 100 万条约 1GB |
| 网络 | 与应用服务器同一个 VPC、同一个地域 |
| 系统 | Ubuntu |

容量参考：2 核 2G 上 NATS 每秒处理上万条消息，CPU 不到 1 核。tenon 应用的瓶颈在 PostgreSQL 和处理函数。内存随积压和定时消息增长，定时消息每条约 0.75KB。

## 单机与集群

单机的影响范围：

| 情况 | 影响 |
|---|---|
| 进程崩溃、重启、升级 | 不丢任务。任务暂停处理，发布不受影响 |
| 断电、宕机 | 丢最近约 1 秒写入的任务 |
| 磁盘损坏 | 丢 NATS 中未处理的任务，包括 `max_defer` 以内的延迟和重试 |
| 广播、在线成员 | 停机期间中断，恢复后自动恢复 |

以下情况用 3 节点集群（`queue.replicas: 3`）：

- 任务丢失后无法从业务数据补回；
- 不允许任务暂停处理。

## 安装

```bash
bash scripts/install-docker.sh            # 完成后重新登录
ALLOW_CIDR=<VPC 网段> bash scripts/install-nats.sh product_a product_b
```

参数是产品的用户名，账号名为其大写。环境变量：

| 变量 | 默认值 | 作用 |
|---|---|---|
| `ALLOW_CIDR` | `172.16.0.0/12` | 允许访问 4222 的网段 |
| `PRIVATE_IP` | eth0 的地址 | 监听地址 |
| `QUOTA_FILE` / `QUOTA_MEM` | `5GB` / `64MB` | 每个账号的 JetStream 配额 |
| `NATS_IMAGE` | `nats:2.15.3-alpine` | 镜像 |

脚本执行的操作：

- 为每个用户和运维用户 `sys` 生成密码，明文存于 `/etc/nats/<用户>.password`（仅 root 可读），配置中只写 bcrypt 哈希。已有密码沿用。
- 生成 `/etc/nats/nats.conf`，旧配置备份为带时间戳的文件。
- 首次运行启动容器 `nats`（host 网络、自动重启、`GOMEMLIMIT=1200MiB`、数据卷 `nats-data`）；之后热加载配置并检查结果。
- ufw 只放行 SSH，4222 只放行 `ALLOW_CIDR`。

检查：

```bash
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'
sudo ufw status
sudo ss -ltnp | grep -E '4222|8222'
```

内存不是 2G 时，按内存的 60–80% 设置 `GOMEMLIMIT`，见 [03 运维](03-operations.md#重建容器)。

## 网络安全

- 公网 IP 经 NAT 映射到内网 IP，只监听内网不能阻止公网访问。安全组只放行应用服务器访问 4222；ufw 作为第二道限制。
- 8222 只监听 127.0.0.1。
- 跨机器需要加密时配置 TLS。
- 本机开着 TUN 代理时，测试公网端口会误显示连通。用一个未监听的端口对照，或检查是否收到 `INFO {...}`。

## 配置

```
listen: <内网 IP>:4222
http: 127.0.0.1:8222

jetstream {
  store_dir: /data
  sync_interval: "1s"
  max_file_store: 30GB
  max_memory_store: 256MB
}
```

- `sync_interval: "1s"`：每秒同步磁盘，吞吐下降 5–10%。默认 2 分钟；`always` 在普通云盘上约 450 条/秒，不用。
- `max_file_store`：磁盘上限。各账号 `max_file` 之和不超过它。
- `max_memory_store`：实时频道的在线成员使用内存存储。
- `server_name` 不能热加载修改。`listen`、`jetstream` 的改动需要重启容器；账号、用户、配额热加载。

## 应用配置

```yaml
nats:
  url: ${NATS_URL}   # nats://product_a:<密码>@<内网 IP>:4222，密码用环境变量传入
queue:
  stream: jobs       # 默认值
  replicas: 1
  max_defer: 10m     # 更晚的延迟和重试存入 PostgreSQL，减少 NATS 内存和迁移量
```

`<stream>.>`（默认 `jobs.>`）只给队列用，其他消息发进去会被存入队列的 stream。
