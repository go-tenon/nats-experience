# 04 迁移

把一个产品从共用 NATS 迁到单独的 NATS。以 `product_a`、stream `jobs` 为例。

worker 停止期间，发布照常进行，新任务留在 PostgreSQL 的复制槽中；worker 连上新 NATS 后自动转发。NATS 中已有的任务、定时消息和 consumer 进度通过备份和恢复迁移。任务暂停处理的时长等于 worker 停止的时长。

## 1. 准备新服务器

1. 按 [01 部署](01-deploy.md) 安装 Docker。
2. 复制旧服务器的 `/etc/nats/product_a.password` 到新服务器同一路径（属主 root，权限 600），保持密码不变。
3. `bash scripts/install-nats.sh product_a`
4. 确认应用服务器能访问新服务器的 4222。

## 2. 缩短 max_defer（可选，提前一天）

把 `queue.max_defer` 改为 1–5 分钟并发布，减少 NATS 中的定时消息。

## 3. 停止 worker

平滑停止该产品的全部 worker（SIGTERM）。发布任务的进程不停。worker 与 HTTP 在同一进程（`start`）时，改为只运行 `serve`。

## 4. 备份和恢复

```bash
mkdir -p ~/natsbackup && chmod 777 ~/natsbackup
nbx() { h=$1; shift; docker run --rm --network host -v ~/natsbackup:/backup \
  -e NATS_URL=nats://$h -e NATS_USER=product_a -e NATS_PASSWORD="$(sudo cat /etc/nats/product_a.password)" \
  natsio/nats-box:latest nats "$@"; }

nbx <旧 IP>:4222 backup stream jobs /backup/jobs --no-progress
nbx <新 IP>:4222 backup restore stream /backup/jobs --no-progress

nbx <旧 IP>:4222 stream info jobs | grep -i messages    # 两边消息数相同
nbx <新 IP>:4222 stream info jobs | grep -i messages
nbx <新 IP>:4222 consumer ls jobs
```

## 5. 切换

1. 应用的 `NATS_URL` 改为新服务器。
2. 启动 worker，重启其他使用 NATS 的进程（广播、实时频道）。
3. 检查：`nbx <新 IP>:4222 consumer report jobs`、tenon 队列面板。

## 6. 清理旧服务器

确认正常后立即执行：

```bash
nbx <旧 IP>:4222 stream rm jobs -f
bash scripts/install-nats.sh <其余产品>    # 不再列出 product_a
sudo rm /etc/nats/product_a.password
sudo rm -rf ~/natsbackup
```

旧 stream 中的定时消息到时仍会触发，worker 误连旧服务器会重复处理任务。

## 回退

第 6 步之前：`NATS_URL` 改回旧服务器，重启 worker。新服务器上未处理的任务需导回旧服务器，或在新服务器上处理完后再切回。
