# 06 迁移：把一个产品从共用的 NATS 搬到单独的 NATS

## 原理

tenon 的任务先写进 PostgreSQL（WAL），再由 worker 转发到 NATS。worker 停止期间，应用照常发布，新任务留在 PostgreSQL 的逻辑复制槽中，不会丢；worker 改连新的 NATS 后，积压的任务自动转发过去。所以迁移只需要停 worker，不需要停应用。

NATS 中已有的数据（等待处理的任务、定时消息、consumer 的进度）用 `nats backup stream` / `nats backup restore stream` 搬过去。

## 步骤

1. 在新机器上部署 NATS（`scripts/ecs/install-nats.sh <用户名>`）。用户名和密码与旧服务器相同（复制 `/etc/nats/<用户>.password` 后再运行脚本），应用只需要改地址。
2. （可选，提前一天）把这个产品的 `queue.max_defer` 调小，减少要搬的定时消息。
3. **平滑停止这个产品的所有 worker。** 发布方（API）照常运行。
4. 备份旧服务器上的 stream，恢复到新服务器：

   ```bash
   mkdir -p ~/natsbackup && chmod 777 ~/natsbackup
   nbx() { h=$1; shift; docker run --rm --network host -v ~/natsbackup:/backup -e NATS_URL=nats://$h -e NATS_USER=product_a -e NATS_PASSWORD="$(sudo cat /etc/nats/product_a.password)" natsio/nats-box:latest nats "$@"; }
   nbx <旧>:4222 backup stream jobs /backup/jobs --no-progress
   nbx <新>:4222 backup restore stream /backup/jobs --no-progress
   ```

   核对两边 `nats stream info jobs` 的消息数相同；consumer 会一起恢复。（`nats stream backup/restore` 已经过时，用 `nats backup ...`。）
5. 把应用的 `NATS_URL` 改成新服务器，启动 worker。
6. **确认新服务器上任务正常处理后，立刻删除旧服务器上的 stream**（`nats stream rm jobs`），再从旧配置中删掉这个账号并 HUP。旧 stream 中的定时消息到时间仍会触发；留着它，误连回旧服务器的 worker 会把任务再处理一遍。
7. 广播、在线成员不需要迁移数据：改地址、重启后自动恢复。

## 演练结果

`scripts/ecs/migration-drill.sh`，在一台机器上用两个 NATS（:4222 旧、:4223 新）模拟；原始结果 `results/ecs/07-migration-drill.txt`。产品 A 以 50 任务/秒持续发布，其中 1/10 延迟 2–3 分钟：

| 时间 | 事件 |
|---|---|
| 0s | worker 连旧服务器，发布方开始 |
| 60s | 平滑停止 worker；旧服务器上 A 的 stream 有 310 条消息（等待处理和定时） |
| 66s | 备份（34KiB 压缩、120KiB 原始）并恢复到新服务器：310 条，consumer `demo_job` 一起恢复 |
| 67s | worker 改连新服务器 |
| 97s | 停止发布方 |
| 约 4.5 分钟 | 所有延迟任务到期并处理 |

| 核对 | 结果 |
|---|---|
| 发布的任务 | 4902（491 个延迟） |
| 处理一次 | 4902 |
| 到期 30 秒后仍未处理（丢失） | 0 |
| 处理多次（重复） | 0 |
| 早于预定时间执行 | 0 |
| 任务暂停处理 | 约 7 秒（停 worker 到新 worker 启动），发布不受影响 |

在旧服务器上已经定好时间的延迟任务，恢复到新服务器后都按时执行了。

新服务器上最后剩 10 条消息：隔离测试时发到 `jobs.test` 的测试消息被队列 stream 收录，随备份迁了过来。这说明了两点：不要往 `jobs.>` 发其他消息；迁移后要清理旧 stream。

## 核对方法

`tools/tenonmig` 的处理函数把业务表中这一行的 `handled` 加一，迁移结束后用一条 SQL 统计 `handled = 0`（丢失）、`handled > 1`（重复）、`done_at < run_at`（提前）。需要证明“恰好一次”时，这类在业务数据上的核对比数消息条数可靠。
