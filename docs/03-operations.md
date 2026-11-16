# 03 运维

| 项目 | 位置 |
|---|---|
| 配置 | `/etc/nats/nats.conf` |
| 密码 | `/etc/nats/<用户>.password` |
| 数据 | 数据卷 `nats-data` |
| 容器 | `nats` |
| 端口 | `<内网 IP>:4222`，监控 `127.0.0.1:8222` |

## 状态

```bash
docker ps --filter name=nats
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'
curl -s 127.0.0.1:8222/varz | head -40
docker logs --tail 100 nats
```

## NATS 命令行

```bash
nb() { u=$1; shift; docker run --rm -i --network host \
  -e NATS_URL=nats://$(ip -4 -o addr show eth0 | awk '{print $4}' | cut -d/ -f1):4222 \
  -e NATS_USER=$u -e NATS_PASSWORD="$(sudo cat /etc/nats/$u.password)" \
  natsio/nats-box:latest nats "$@"; }

nb product_a stream ls
nb product_a consumer report jobs     # 各 topic 的积压和处理中任务
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
sudo cp /etc/nats/nats.conf /etc/nats/nats.conf.bak
sudo vi /etc/nats/nats.conf
docker kill -s HUP nats          # 账号、用户、配额
docker restart nats              # listen、jetstream
```

## 更换密码

```bash
U=product_a
openssl rand -base64 60 | tr -dc 'A-Za-z0-9' | head -c 40 | sudo tee /etc/nats/$U.password >/dev/null
PASSWORD="$(sudo cat /etc/nats/$U.password)" docker run --rm -e PASSWORD natsio/nats-box:latest nats server passwd
# 用输出的哈希替换 nats.conf 中该用户的 password
docker kill -s HUP nats
```

热加载后旧密码失效，立即更新应用的 `NATS_URL`。

## 重建容器

```bash
docker rm -f nats
docker run -d --name nats --restart always --network host \
  -e GOMEMLIMIT=1200MiB --log-opt max-size=50m --log-opt max-file=3 \
  -v /etc/nats/nats.conf:/etc/nats/nats.conf:ro -v nats-data:/data \
  nats:2.15.3-alpine nats-server -c /etc/nats/nats.conf
```

数据在数据卷中，不受影响。

## 升级

1. 阅读[发布说明](https://github.com/nats-io/nats-server/releases)。
2. 磁盘快照。
3. `docker pull nats:<新版本>-alpine`，用新镜像重建容器。
4. 检查 `healthz` 和 `nb <用户> consumer report jobs`。

## 备份

- 磁盘自动快照，每天一次。
- 单个产品：`nats backup stream jobs <目录>`，见 [04 迁移](04-migration.md)。

## 监控

- tenon：`queue.relay.active` 所有实例之和为 0 时告警；`queue.relay.lag`；队列面板中“没有 Worker 在处理”“没有注册处理函数”的 topic。
- 主机：磁盘、内存、`healthz`。可用 [prometheus-nats-exporter](https://github.com/nats-io/prometheus-nats-exporter) 采集 `127.0.0.1:8222`。

## 注意

- 修改 ufw 前先 `sudo ufw allow OpenSSH`，改完用新连接确认能登录。
- nats-box 在挂载目录中创建的文件属于 root，用 `sudo` 删除。
