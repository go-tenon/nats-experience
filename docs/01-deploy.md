# 01 部署

## 机器

Ubuntu x86_64，2 核 2G 起，ESSD 并开启自动快照，与应用服务器同一地域（优先同一 VPC）。

2 核 2G 上 NATS 每秒处理上万条消息，CPU 不到 1 核。内存随积压和定时消息增长，定时消息每条约 0.75KB。磁盘按最大积压计算：1KB 任务每 100 万条约 1GB。

故障影响：

| 情况 | 影响 |
|---|---|
| 进程崩溃、重启、升级 | 不丢任务。任务暂停处理，发布不受影响 |
| 断电、宕机 | 丢最近约 1 秒写入的任务 |
| 磁盘损坏 | 丢 NATS 中未处理的任务，包括 `max_defer` 以内的延迟和重试 |

## 安装

在管理员用户下执行。

```bash
# 软件
curl -fLO https://github.com/nats-io/nats-server/releases/download/v2.15.3/nats-server-v2.15.3-amd64.deb
curl -fLO https://github.com/nats-io/nsc/releases/download/v2.15.0/nsc-v2.15.0-amd64.deb
curl -fLO https://github.com/nats-io/natscli/releases/download/v0.5.1/nats-0.5.1-amd64.deb
sudo apt install -y ./nats-server-v2.15.3-amd64.deb ./nsc-v2.15.0-amd64.deb ./nats-0.5.1-amd64.deb
git clone git@github.com:go-tenon/nats-experience.git && cd nats-experience

# 用户和目录
sudo useradd --system --home-dir /var/lib/nats --shell /usr/sbin/nologin nats
sudo install -d -o nats -g nats -m 750 /var/lib/nats /var/log/nats
sudo install -d -m 755 /etc/nats

# 运营者
nsc add operator --name tenon --sys
nsc edit operator --account-jwt-server-url nats://127.0.0.1:4222
nats context add sys --server nats://127.0.0.1:4222 --creds ~/.local/share/nats/nsc/keys/creds/tenon/SYS/sys.creds

# 配置
nsc generate config --nats-resolver --sys-account SYS \
  | sed "s|dir: './jwt'|dir: '/var/lib/nats/jwt'|; s|allow_delete: false|allow_delete: true|" \
  | sudo tee /etc/nats/resolver.conf >/dev/null
sudo cp etc/nats/nats.conf.example /etc/nats/nats.conf          # 修改 server_name
sudo chown root:nats /etc/nats/*.conf && sudo chmod 640 /etc/nats/*.conf
sudo -u nats nats-server -t -c /etc/nats/nats.conf

# 服务
sudo cp etc/systemd/system/nats-server.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now nats-server
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'
```

按 [02 账号](02-accounts.md) 添加产品。

## 认证

运营者 `tenon` 签发账号，服务器只信任运营者；账号对应产品；用户属于账号，持有 `.creds` 连接。账号和用户用 `nsc` 管理，推送后生效，不改服务器配置。

nsc 数据在管理员用户下：

| 路径 | 内容 |
|---|---|
| `~/.local/share/nats/nsc/stores` | 运营者、账号、用户的 JWT |
| `~/.local/share/nats/nsc/keys` | 私钥；用户的 `.creds` 在 `creds/tenon/<账号>/<用户>.creds` |
| `~/.config/nats` | nsc 设置、context |

**这三个目录要备份到离线或加密存储**：私钥丢失后无法再签发，泄露后他人可签发凭据。

## 访问控制

- 4222 监听 `0.0.0.0`，在云平台安全组中放行应用服务器。
- 8222（监控）只监听 127.0.0.1。

## 配置说明

| 项目 | 说明 |
|---|---|
| `sync_interval: "1s"` | 每秒同步磁盘，吞吐下降 5–10%。`always` 在普通云盘上约 450 条/秒，不用 |
| `max_file_store: 30GB` | 所有账号合计的磁盘上限。账号不设配额 |
| `max_memory_store: 256MB` | 实时频道的在线成员使用内存存储 |
| `log_file` | `/var/log/nats/nats-server.log`，100MB 轮转，保留 10 个 |
| `GOMEMLIMIT` | 在 `nats-server.service` 中，取内存的 60–80% |

## 应用配置

```yaml
nats:
  url: nats://<NATS 地址>:4222
  credentials: /etc/app/nats.creds   # 产品用户的 .creds，权限 600
queue:
  max_defer: 10m                     # 更晚的延迟和重试存入 PostgreSQL，减少 NATS 内存和迁移量
```

`jobs.>`（`queue.stream` 的 subject 范围）只给队列用。
