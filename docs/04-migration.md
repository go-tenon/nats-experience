# 04 迁移：把一个产品搬到单独的 NATS

某个产品流量变大、影响到共用 NATS 上的其他产品时，把它迁到单独的机器。

## 原理

tenon 的任务先写进 PostgreSQL，再由 worker 转发到 NATS。worker 停止期间，应用照常发布，新任务留在 PostgreSQL 的逻辑复制槽中，不会丢；worker 改连新的 NATS 后，积压的任务自动转发过去。所以迁移只需要停 worker，不需要停应用。

NATS 中已有的数据（等待处理的任务、定时投递的延迟任务和重试、consumer 的进度）用 `nats backup stream` 导出，再导入新服务器。

在实验环境中演练过：迁移期间应用持续发布，所有任务（包括迁移前已经在旧服务器上定好时间的延迟任务）都恰好处理了一次，任务暂停处理的时间就是停 worker 到新 worker 启动的间隔（演练中约 7 秒）。

## 步骤

以产品 `product_a`、stream `jobs` 为例。

### 1. 准备新服务器（提前）

1. 按 [01 部署](01-deploy.md) 准备新机器和 Docker。
2. 把旧服务器上这个产品的密码文件复制到新服务器的 `/etc/nats/product_a.password`（权限 600，属主 root），让用户名和密码保持不变，应用只需要改地址。
3. 在新服务器上运行 `bash scripts/install-nats.sh product_a`。
4. 确认应用服务器能连到新服务器的 4222（安全组、ufw 的 `ALLOW_CIDR`）。

### 2. 减少要搬的数据（可选，提前一天）

把这个产品的 `queue.max_defer` 调小（例如 1–5 分钟）并发布，让较晚的延迟任务和重试存在 PostgreSQL 中，NATS 中的定时消息变少。

### 3. 停止 worker

平滑停止这个产品的**所有** worker 进程（发送 SIGTERM，等它们退出）。API 等发布任务的进程照常运行。

如果 worker 和 HTTP 服务在同一个进程中（`start` 命令），改为只运行 HTTP 的进程（`serve`），或者在维护窗口中进行。

### 4. 导出和导入

在能同时访问两台服务器的机器上（例如旧服务器）：

```bash
mkdir -p ~/natsbackup && chmod 777 ~/natsbackup
nbx() { h=$1; shift; docker run --rm --network host -v ~/natsbackup:/backup \
  -e NATS_URL=nats://$h -e NATS_USER=product_a -e NATS_PASSWORD="$(sudo cat /etc/nats/product_a.password)" \
  natsio/nats-box:latest nats "$@"; }

nbx <旧服务器内网 IP>:4222 backup stream jobs /backup/jobs --no-progress
nbx <新服务器内网 IP>:4222 backup restore stream /backup/jobs --no-progress

# 核对：两边的消息数相同，consumer 也已恢复
nbx <旧服务器内网 IP>:4222 stream info jobs | grep -i messages
nbx <新服务器内网 IP>:4222 stream info jobs | grep -i messages
nbx <新服务器内网 IP>:4222 consumer ls jobs
```

（`nats stream backup/restore` 已经过时，用 `nats backup stream` 和 `nats backup restore stream`。）

### 5. 切换

1. 把应用的 `NATS_URL` 改成新服务器的地址。
2. 启动 worker；发布任务的进程也要重启以使用新地址（tenon 中发布只写 PostgreSQL，不连 NATS，但同一个进程中的广播、实时频道会用到它）。
3. 检查新服务器上任务在处理：`nbx <新服务器>:4222 consumer report jobs`，以及 tenon 的队列面板。

### 6. 清理旧服务器（确认正常后立即做）

```bash
nbx <旧服务器内网 IP>:4222 stream rm jobs -f
```

再从旧服务器的配置中去掉这个产品（重新运行 `install-nats.sh` 时不再列出它），删除它的密码文件，删除备份目录（`sudo rm -rf ~/natsbackup`）。

**旧 stream 不能留着**：其中的定时消息到时间仍会触发，如果有 worker 误连回旧服务器，这些任务会被处理第二次。

### 7. 广播和实时频道

不需要迁移数据：应用改地址、重启后自动恢复。

## 回退

切换后发现问题时，把 `NATS_URL` 改回旧服务器、重启 worker 即可（前提是第 6 步还没做）。新服务器上这段时间转发过去、还没处理的任务需要再导出导入回旧服务器，或者等它们在新服务器上处理完后再切回。
