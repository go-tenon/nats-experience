# 02 运维

命令都在 `nats` 目录中执行。

## 命令行

`nats` 命令行在 `cli` 容器中运行，不用安装。在 `nats` 目录中设置别名，之后在任何目录都能用：

```bash
echo "alias nats='sudo docker compose -f $PWD/compose.yaml run --rm -q cli'" >> ~/.bashrc && . ~/.bashrc
```

为系统账号和每个产品各建一个 context，密码见 `config/nats.conf`。context 保存在 `cli/` 中，只需建一次：

```bash
nats context add sys --server nats://nats:4222 --user admin --password <密码>
nats context add product_a --server nats://nats:4222 --user product_a --password <密码>
```

## 状态

```bash
sudo docker compose ps                                  # nats 为 healthy
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'
sudo docker compose logs --tail 100 nats
nats --context product_a consumer report jobs           # 各 topic 的积压和处理中任务
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

`config/nats.conf` 修改后检查语法，再重新加载，不中断连接：

```bash
sudo docker compose exec nats nats-server -t -c /etc/nats/nats.conf
sudo docker compose kill -s HUP nats
sudo docker compose logs --tail 3 nats
```

`server_name` 和监听地址不能热加载，修改后执行 `sudo docker compose up -d --force-recreate nats`。`compose.yaml` 修改后执行 `sudo docker compose up -d`。

## 升级

磁盘快照 → 修改 `compose.yaml` 中的镜像版本 → `sudo docker compose up -d --wait`。重启期间任务暂停处理，发布不受影响。

## 备份

- 磁盘自动快照，每天一次，包括 `data/` 和两个文件。
- `config/nats.conf` 中是所有产品的密码，另外保存到离线或加密存储。

## 监控

- tenon：`queue.relay.active` 所有实例之和为 0 时告警；`queue.relay.lag`；队列面板。
- NATS 和主机：[04 接入 OpenObserve](04-openobserve.md)。
