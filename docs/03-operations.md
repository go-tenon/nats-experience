# 03 运维

在部署目录（`~/nats`）中执行，`nb` 见 [01 部署](01-deploy.md#安装)。

| 项目 | 位置 |
|---|---|
| 配置 | `nats.conf`、`resolver.conf` |
| 签名私钥、凭据、context | `admin/` |
| 数据、账号 JWT | 数据卷 `nats-data` |
| 端口 | 4222；监控 `127.0.0.1:8222` |

## 状态

```bash
docker compose ps
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'
curl -s 127.0.0.1:8222/varz | head -40
docker compose logs --tail 100 nats
```

## NATS 命令行

用 context 指定账号：

```bash
nb nats context ls
nb nats --context product_a stream ls
nb nats --context product_a consumer report jobs     # 各 topic 的积压和处理中任务
nb nats --context sys server report jetstream        # 全局汇总
```

添加 `sys` context：

```bash
nb nats context add sys --server nats://127.0.0.1:4222 --creds /nsc/nkeys/creds/tenon/SYS/sys.creds
```

tenon 的 topic 对应同名 consumer，`.` 换成 `_`：`mail.send` → `mail_send`。

## 暂停 topic

暂停后任务照常发布和保存，worker 不领取新任务，重启后仍暂停：

```bash
nb nats --context product_a consumer pause jobs mail_send 1h -f     # 或具体时间 "2026-12-01 08:00:00"
nb nats --context product_a consumer resume jobs mail_send -f
```

`-f` 必须加：`nb` 没有终端，无法确认。tenon 队列面板也可以暂停和恢复。

## 修改配置

```bash
cp nats.conf nats.conf.bak
vi nats.conf
docker compose restart nats
```

账号和用户的变更用 `nsc push`，不改配置文件，见 [02 账号](02-accounts.md)。

## 升级

1. 阅读[发布说明](https://github.com/nats-io/nats-server/releases)。
2. 磁盘快照。
3. 修改 `compose.yaml` 中的镜像版本，`docker compose up -d`。
4. 检查 `healthz` 和 `nb nats --context <产品> consumer report jobs`。

升级期间任务暂停处理，发布不受影响。

## 备份

- 磁盘自动快照，每天一次。
- `admin/`：变更账号或用户后备份，存放在离线或加密存储。
- 单个产品：`nats backup stream jobs <目录>`，见 [04 迁移](04-migration.md)。

## 监控

- tenon：`queue.relay.active` 所有实例之和为 0 时告警；`queue.relay.lag`；队列面板中“没有 Worker 在处理”“没有注册处理函数”的 topic。
- NATS：见 [05 接入 OpenObserve](05-openobserve.md)。

## 注意

- `nb` 以当前用户运行，`admin/` 中的文件属于当前用户。不要用 `sudo` 执行 `nb`。
- nats-box 在其他挂载目录中创建的文件属于 root，用 `sudo` 删除。
