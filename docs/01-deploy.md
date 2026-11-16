# 01 部署

## 机器

| 项目 | 要求 |
|---|---|
| 规格 | 2 核 2G 起；对延迟稳定性有要求时用独享型实例 |
| 磁盘 | ESSD，开启自动快照。容量按最大积压计算：1KB 任务每 100 万条约 1GB |
| 网络 | 与应用服务器同一地域，优先同一 VPC |
| 系统 | Ubuntu x86_64 |

容量参考：2 核 2G 上 NATS 每秒处理上万条消息，CPU 不到 1 核。tenon 应用的瓶颈在 PostgreSQL 和处理函数。内存随积压和定时消息增长，定时消息每条约 0.75KB。

## 单机与集群

| 情况 | 影响 |
|---|---|
| 进程崩溃、重启、升级 | 不丢任务。任务暂停处理，发布不受影响 |
| 断电、宕机 | 丢最近约 1 秒写入的任务 |
| 磁盘损坏 | 丢 NATS 中未处理的任务，包括 `max_defer` 以内的延迟和重试 |
| 广播、在线成员 | 停机期间中断，恢复后自动恢复 |

以下情况用 3 节点集群（`queue.replicas: 3`）：

- 任务丢失后无法从业务数据补回；
- 不允许任务暂停处理。

## 认证

JWT + NKey：

- **运营者**（`tenon`）签发账号，服务器只信任运营者。
- **账号**对应产品，subject 和 stream 相互隔离。
- **用户**属于账号，持有 `.creds`（JWT + 私钥）连接，私钥不在网络上传输。

账号和用户用 `nsc` 管理，推送到服务器后生效，不改服务器配置、不重启。`nsc` 在管理员用户下运行，数据在：

| 路径 | 内容 |
|---|---|
| `~/.local/share/nats/nsc/stores` | 运营者、账号、用户的 JWT |
| `~/.local/share/nats/nsc/keys` | 签名私钥，用户的 `.creds`（`creds/tenon/<账号>/<用户>.creds`） |
| `~/.config/nats` | nsc 设置，nats 命令行的 context |

**备份这三个目录**：签名私钥丢失后无法再签发账号和用户，泄露后他人可签发凭据。备份到离线或加密存储。

## 安装

### 软件

```bash
curl -fLO https://github.com/nats-io/nats-server/releases/download/v2.15.3/nats-server-v2.15.3-amd64.deb
curl -fLO https://github.com/nats-io/nsc/releases/download/v2.15.0/nsc-v2.15.0-amd64.deb
curl -fLO https://github.com/nats-io/natscli/releases/download/v0.5.1/nats-0.5.1-amd64.deb
sudo apt install -y ./nats-server-v2.15.3-amd64.deb ./nsc-v2.15.0-amd64.deb ./nats-0.5.1-amd64.deb
```

### 仓库

```bash
git clone git@github.com:go-tenon/nats-experience.git && cd nats-experience
```

以下命令在仓库目录中执行。

### 用户和目录

```bash
sudo useradd --system --home-dir /var/lib/nats --shell /usr/sbin/nologin nats
sudo install -d -o nats -g nats -m 750 /var/lib/nats /var/log/nats
sudo install -d -m 755 /etc/nats
```

### 运营者

```bash
nsc add operator --name tenon --generate-signing-key --sys
nsc edit operator --require-signing-keys --account-jwt-server-url nats://127.0.0.1:4222
nats context add sys --server nats://127.0.0.1:4222 --creds ~/.local/share/nats/nsc/keys/creds/tenon/SYS/sys.creds
```

### 配置

```bash
nsc generate config --nats-resolver --sys-account SYS \
  | sed "s|dir: './jwt'|dir: '/var/lib/nats/jwt'|; s|allow_delete: false|allow_delete: true|" \
  | sudo tee /etc/nats/resolver.conf >/dev/null
sudo cp etc/nats/nats.conf.example /etc/nats/nats.conf          # 修改 server_name
sudo chown root:nats /etc/nats/*.conf && sudo chmod 640 /etc/nats/*.conf
sudo -u nats nats-server -t -c /etc/nats/nats.conf
```

`resolver.conf` 包含运营者 JWT、系统账号和 resolver 设置；账号 JWT 存于 `/var/lib/nats/jwt`。

### 服务

```bash
sudo cp etc/systemd/system/nats-server.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now nats-server
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'
sudo grep -A1 'Trusted Operators' /var/log/nats/nats-server.log     # Operator: "tenon"
```

按 [02 账号](02-accounts.md) 添加产品。

## 访问控制

- 4222 监听 `0.0.0.0`，在云平台安全组中按 IP 或网段放行应用服务器。
- 同一 VPC 的应用用内网地址连接；其他应用用公网地址，安全组只放行其 IP。
- 8222（监控）只监听 127.0.0.1，不对外开放。

## 配置

| 项目 | 值 |
|---|---|
| `sync_interval: "1s"` | 每秒同步磁盘，吞吐下降 5–10%。默认 2 分钟；`always` 在普通云盘上约 450 条/秒，不用 |
| `max_file_store: 30GB` | 所有账号合计的磁盘上限。账号不设配额，用满时所有产品都无法写入 |
| `max_memory_store: 256MB` | 实时频道的在线成员使用内存存储 |
| `log_file` | `/var/log/nats/nats-server.log`，100MB 轮转，保留 10 个 |
| `GOMEMLIMIT` | 在 `nats-server.service` 中，取内存的 60–80% |

## 应用配置

```yaml
nats:
  url: nats://<NATS 地址>:4222
  credentials: /etc/app/nats.creds   # 产品用户的 .creds，权限 600
queue:
  stream: jobs       # 默认值
  replicas: 1
  max_defer: 10m     # 更晚的延迟和重试存入 PostgreSQL，减少 NATS 内存和迁移量
```

`<stream>.>`（默认 `jobs.>`）只给队列用，其他消息发进去会被存入队列的 stream。
