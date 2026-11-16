# 04 迁移

把一个产品从共用 NATS 迁到单独的 NATS。以账号 `PRODUCT_A`、stream `jobs` 为例。

新服务器使用同一个运营者，应用的 `.creds` 不变，只改地址。worker 停止期间，发布照常进行，新任务留在 PostgreSQL 的复制槽中；worker 连上新 NATS 后自动转发。NATS 中已有的任务、定时消息和 consumer 进度通过备份和恢复迁移。任务暂停处理的时长等于 worker 停止的时长。

以下命令在旧服务器的部署目录中执行，`<新地址>` 为新服务器的内网地址。

## 1. 准备新服务器

1. 安装 Docker，复制部署目录中的 `compose.yaml`、`nats.conf`、`resolver.conf`（**不复制 `admin/`**），`nats.conf` 中的 `server_name` 改为新机器名。
2. `docker compose up -d`。
3. 安全组放行应用服务器和旧服务器访问新服务器的 4222。
4. 推送账号：`nb nsc push --account PRODUCT_A --account-jwt-server-url nats://<新地址>:4222`
5. 添加 context：`nb nats context add product_a_new --server nats://<新地址>:4222 --creds /nsc/nkeys/creds/tenon/PRODUCT_A/app.creds`

## 2. 缩短 max_defer（可选，提前一天）

把 `queue.max_defer` 改为 1–5 分钟并发布，减少 NATS 中的定时消息。

## 3. 停止 worker

平滑停止该产品的全部 worker（SIGTERM）。发布任务的进程不停。worker 与 HTTP 在同一进程（`start`）时，改为只运行 `serve`。

## 4. 备份和恢复

```bash
nb nats --context product_a backup stream jobs /nsc/backup/jobs --no-progress
nb nats --context product_a_new backup restore stream /nsc/backup/jobs --no-progress

nb nats --context product_a stream info jobs | grep -i messages        # 两边消息数相同
nb nats --context product_a_new stream info jobs | grep -i messages
nb nats --context product_a_new consumer ls jobs
```

## 5. 切换

1. 应用的 `nats.url` 改为新服务器，`credentials` 不变。
2. 启动 worker，重启其他使用 NATS 的进程（广播、实时频道）。
3. 检查：`nb nats --context product_a_new consumer report jobs`、tenon 队列面板。

## 6. 清理旧服务器

确认正常后立即执行：

```bash
nb nats --context product_a stream rm jobs -f
nb nsc push --account-removal PRODUCT_A        # 只从旧服务器删除账号
rm -rf admin/backup
nb nats context rm product_a -f
```

旧 stream 中的定时消息到时仍会触发，worker 误连旧服务器会重复处理任务。

此后 `PRODUCT_A` 的推送须指定新服务器：`nb nsc push --account PRODUCT_A --account-jwt-server-url nats://<新地址>:4222`。

## 回退

第 6 步之前：`nats.url` 改回旧服务器，重启 worker。新服务器上未处理的任务需导回旧服务器，或在新服务器上处理完后再切回。
