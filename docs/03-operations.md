# 03 日常运维

## 位置

| 项目 | 位置 |
|---|---|
| 配置 | `/etc/nats/nats.conf`（备份是同目录下带时间戳的 `.bak`） |
| 密码 | `/etc/nats/<用户>.password`，只有 root 可读 |
| 数据 | Docker 数据卷 `nats-data` |
| 容器 | `nats`，host 网络，`--restart always` |
| 客户端端口 | `<内网 IP>:4222` |
| 监控端口 | `127.0.0.1:8222` |

## 状态

```bash
docker ps --filter name=nats
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'
curl -s 127.0.0.1:8222/varz | head -40          # 连接数、内存、版本
curl -s '127.0.0.1:8222/jsz?accounts=true'      # 各账号的 JetStream 用量
docker logs --tail 100 nats
```

## NATS 命令行

用 nats-box 容器，不需要安装。按用户连接，密码从文件读取，不出现在命令行参数里：

```bash
nb() { u=$1; shift; docker run --rm -i --network host \
  -e NATS_URL=nats://$(ip -4 -o addr show eth0 | awk '{print $4}' | cut -d/ -f1):4222 \
  -e NATS_USER=$u -e NATS_PASSWORD="$(sudo cat /etc/nats/$u.password)" \
  natsio/nats-box:latest nats "$@"; }

nb product_a stream ls
nb product_a stream info jobs
nb product_a consumer report jobs               # 每个 topic 的积压（Unprocessed）和处理中（Ack Pending）
nb sys server report jetstream                  # 全局汇总
```

tenon 中每个 topic 对应一个 consumer，名字是把 topic 中的 `.` 换成 `_`，例如 `mail.send` 是 `mail_send`。

### 暂停和恢复一个 topic

下游服务出故障时，先暂停对应的 topic，免得任务大量失败转入失败任务表。任务照常发布和保存，worker 不再领取新任务；worker 重启后仍然暂停。

```bash
nb product_a consumer pause jobs mail_send 1h -f     # 1h 也可以写成具体时间，例如 "2026-12-01 08:00:00"
nb product_a consumer resume jobs mail_send -f
```

`-f` 跳过确认：`nb` 没有分配终端，不加时命令会因为无法确认而失败。

也可以在 tenon 的队列面板上操作（暂停 1 小时、恢复）。

## 修改配置

```bash
sudo cp /etc/nats/nats.conf /etc/nats/nats.conf.bak
sudo vi /etc/nats/nats.conf
```

- 账号、用户、配额的改动：`docker kill -s HUP nats`，再用 `docker logs --since 1m nats | grep -i reload` 确认出现 `Reloaded server configuration`。
- `listen`、`jetstream` 段的改动：`docker restart nats`。重启期间任务暂停处理，不会丢。
- `server_name` 热加载时不能修改。

## 更换密码

```bash
U=product_a
openssl rand -base64 60 | tr -dc 'A-Za-z0-9' | head -c 40 | sudo tee /etc/nats/$U.password >/dev/null
# 生成哈希：明文通过环境变量传入
PASSWORD="$(sudo cat /etc/nats/$U.password)" docker run --rm -e PASSWORD natsio/nats-box:latest nats server passwd
# 把输出的哈希替换 nats.conf 中这个用户的 password，然后热加载
docker kill -s HUP nats
```

或者删除这个用户的密码文件后重新运行 `install-nats.sh`（参数列出全部产品），脚本会生成新密码。

旧密码失效后应用会连不上，先准备好应用的新 `NATS_URL`，热加载后立即更新应用。

## 重建容器

升级镜像、调整 `GOMEMLIMIT` 时用。数据在数据卷中，重建不会丢失：

```bash
docker rm -f nats
docker run -d --name nats --restart always --network host \
  -e GOMEMLIMIT=1200MiB --log-opt max-size=50m --log-opt max-file=3 \
  -v /etc/nats/nats.conf:/etc/nats/nats.conf:ro -v nats-data:/data \
  nats:2.15.3-alpine nats-server -c /etc/nats/nats.conf
```

## 升级

1. 看 [NATS 的发布说明](https://github.com/nats-io/nats-server/releases)，确认没有不兼容的变更。
2. 在控制台给磁盘打快照。
3. `docker pull nats:<新版本>-alpine`，按上面的命令重建容器，镜像换成新版本。
4. 检查 `healthz` 和日志，确认各产品的 worker 重新连上（tenon 的队列面板或 `nb <用户> consumer report jobs`）。

升级期间任务暂停处理，发布不受影响，不会丢任务。

## 备份

- 在控制台为磁盘设置自动快照策略，例如每天一次。快照只能恢复到快照时刻，之后写入的任务需要从业务数据补回。
- 单个产品的数据可以导出：`nats backup stream jobs <目录>`（见 [04 迁移](04-migration.md)）。

## 监控

- tenon 应用侧：`queue.relay.active`（所有实例之和为 0 时没有 worker 在转发，应当告警）、`queue.relay.lag`；队列面板中状态为“没有 Worker 在处理”“没有注册处理函数”的 topic。
- 这台机器：磁盘使用率（JetStream 上限 `max_file_store`）、内存、`healthz`。可以接入云监控，或用 [prometheus-nats-exporter](https://github.com/nats-io/prometheus-nats-exporter) 采集 `127.0.0.1:8222`。

## 注意事项

- 开启或修改 ufw 之前先放行 SSH（`sudo ufw allow OpenSSH`），改完用一个新的 SSH 连接确认能登录。
- 通过 nats-box 挂载目录（例如备份）时，容器创建的文件属于 root，之后要用 `sudo` 删除。
- 不要往队列的 subject 范围（默认 `jobs.>`）发布其他消息。
