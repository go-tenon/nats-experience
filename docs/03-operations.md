# 03 运维

在部署目录（`~/nats`）中执行。

| 项目 | 位置 |
|---|---|
| 配置 | `nats.conf` |
| 数据 | 数据卷 `nats-data` |
| 端口 | 4222；监控 `127.0.0.1:8222` |

## 状态

```bash
docker compose ps
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'
curl -s 127.0.0.1:8222/varz | head -40
docker compose logs --tail 100 nats
```

## NATS 命令行

```bash
nb() { u=$1; shift; docker run --rm -i --network host \
  -e NATS_URL=nats://127.0.0.1:4222 -e NATS_USER=$u -e NATS_PASSWORD \
  natsio/nats-box:latest nats "$@"; }

export NATS_PASSWORD=<product_a 的密码>
nb product_a stream ls
nb product_a consumer report jobs     # 各 topic 的积压和处理中任务

export NATS_PASSWORD=<sys 的密码>
nb sys server report jetstream
```

tenon 的 topic 对应同名 consumer，`.` 换成 `_`：`mail.send` → `mail_send`。

## 暂停 topic

暂停后任务照常发布和保存，worker 不领取新任务，重启后仍暂停：

```bash
nb product_a consumer pause jobs mail_send 1h -f     # 或具体时间 "2026-12-01 08:00:00"
nb product_a consumer resume jobs mail_send -f
```

`-f` 必须加：`nb` 没有终端，无法确认。tenon 队列面板也可以暂停和恢复。

## 修改配置

```bash
cp nats.conf nats.conf.bak
vi nats.conf
docker compose kill -s HUP nats       # 账号、用户、配额
docker compose restart nats           # listen、jetstream
```

热加载后用 `docker compose logs --since 1m nats | grep -i reload` 确认出现 `Reloaded server configuration`。

## 更换密码

按 [02 账号](02-accounts.md#生成密码和哈希) 生成新密码和哈希，替换 `nats.conf` 中该用户的 `password`，热加载。旧密码立即失效，同时更新应用的 `NATS_URL`。

## 升级

1. 阅读[发布说明](https://github.com/nats-io/nats-server/releases)。
2. 磁盘快照。
3. 修改 `compose.yaml` 中的镜像版本，`docker compose up -d`。
4. 检查 `healthz` 和 `nb <用户> consumer report jobs`。

升级期间任务暂停处理，发布不受影响。

## 备份

- 磁盘自动快照，每天一次。
- 单个产品：`nats backup stream jobs <目录>`，见 [04 迁移](04-migration.md)。

## 监控

- tenon：`queue.relay.active` 所有实例之和为 0 时告警；`queue.relay.lag`；队列面板中“没有 Worker 在处理”“没有注册处理函数”的 topic。
- NATS：见 [05 接入 OpenObserve](05-openobserve.md)。

## 注意

- nats-box 在挂载目录中创建的文件属于 root，用 `sudo` 删除。
