# 03 运维

| 项目 | 位置 |
|---|---|
| 配置 | `/etc/nats/nats.conf`、`/etc/nats/resolver.conf` |
| 数据、账号 JWT | `/var/lib/nats` |
| 日志 | `/var/log/nats/nats-server.log` |
| 服务 | `nats-server.service` |
| nsc、context | 管理员用户的 `~/.local/share/nats/nsc`、`~/.config/nats` |
| 端口 | 4222；监控 `127.0.0.1:8222` |

## 状态

```bash
systemctl status nats-server
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'
curl -s 127.0.0.1:8222/varz | head -40
sudo tail -100 /var/log/nats/nats-server.log
```

## NATS 命令行

```bash
nats context ls
nats --context product_a stream ls
nats --context product_a consumer report jobs     # 各 topic 的积压和处理中任务
nats --context sys server report jetstream        # 全局汇总
```

tenon 的 topic 对应同名 consumer，`.` 换成 `_`：`mail.send` → `mail_send`。

## 暂停 topic

暂停后任务照常发布和保存，worker 不领取新任务，重启后仍暂停：

```bash
nats --context product_a consumer pause jobs mail_send 1h     # 或具体时间 "2026-12-01 08:00:00"
nats --context product_a consumer resume jobs mail_send
```

tenon 队列面板也可以暂停和恢复。

## 修改配置

```bash
sudo cp -p /etc/nats/nats.conf /etc/nats/nats.conf.bak
sudo vi /etc/nats/nats.conf
sudo -u nats nats-server -t -c /etc/nats/nats.conf.bak --test_reload /etc/nats/nats.conf
sudo systemctl reload nats-server       # 输出 can be reloaded
sudo systemctl restart nats-server      # 输出 config reload not supported
```

`listen`、`jetstream`、`server_name`、日志轮转等改动不能热加载。

账号和用户的变更用 `nsc push`，不改配置文件，见 [02 账号](02-accounts.md)。

## 升级

1. 阅读[发布说明](https://github.com/nats-io/nats-server/releases)。
2. 磁盘快照。
3. 安装新版本的 `.deb`，`sudo systemctl restart nats-server`。
4. 检查 `healthz` 和 `nats --context <产品> consumer report jobs`。

升级期间任务暂停处理，发布不受影响。

## 备份

- 磁盘自动快照，每天一次。
- nsc 数据：变更账号或用户后备份 `~/.local/share/nats/nsc`、`~/.config/nats`，存放在离线或加密存储。
- 单个产品：`nats backup stream jobs <目录>`，见 [04 迁移](04-migration.md)。

## 监控

- tenon：`queue.relay.active` 所有实例之和为 0 时告警；`queue.relay.lag`；队列面板中“没有 Worker 在处理”“没有注册处理函数”的 topic。
- NATS：见 [05 接入 OpenObserve](05-openobserve.md)。
