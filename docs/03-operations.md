# 03 运维

| 项目 | 位置 |
|---|---|
| 配置 | `/etc/nats/nats.conf`、`/etc/nats/resolver.conf` |
| 数据、账号 JWT | `/var/lib/nats` |
| 日志 | `/var/log/nats/nats-server.log` |
| nsc、context | 管理员用户的 `~/.local/share/nats/nsc`、`~/.config/nats` |

## 状态

```bash
systemctl status nats-server
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'
sudo tail -100 /var/log/nats/nats-server.log
nats --context product_a consumer report jobs     # 各 topic 的积压和处理中任务
```

tenon 的 topic 对应同名 consumer，`.` 换成 `_`：`mail.send` → `mail_send`。

## 暂停 topic

任务照常发布和保存，worker 不领取新任务：

```bash
nats --context product_a consumer pause jobs mail_send 1h
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

账号和用户的变更用 `nsc push`，不改配置文件。

## 升级

磁盘快照 → 安装新版本的 `.deb` → `sudo systemctl restart nats-server` → 检查 `healthz`。期间任务暂停处理，发布不受影响。

## 备份

- 磁盘自动快照，每天一次。
- 变更账号或用户后备份 `~/.local/share/nats/nsc`、`~/.config/nats`。

## 监控

- tenon：`queue.relay.active` 所有实例之和为 0 时告警；`queue.relay.lag`；队列面板。
- NATS：[05 接入 OpenObserve](05-openobserve.md)。
