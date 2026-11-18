# 03 迁移

把产品 `PRODUCT_A` 迁到单独的 NATS。账号和密码不变，应用只改地址。

worker 停止期间发布照常进行，新任务留在 PostgreSQL 中，worker 连上新 NATS 后自动转发；NATS 中已有的任务、定时消息和 consumer 进度通过备份恢复迁移。任务暂停处理的时长等于 worker 停止的时长。

## 1. 准备新服务器

1. 按 [安装](../README.md#安装) 部署。`nats.conf` 的 `accounts` 只保留 `SYS` 和 `PRODUCT_A` 两段，`PRODUCT_A` 的用户和密码与旧服务器相同。
2. 安全组放行应用服务器和旧服务器访问新服务器的 4222。
3. 在旧服务器上为新服务器建一个 context：

   ```bash
   nats context add product_a_new --server nats://<新地址>:4222 --user product_a --password <密码>
   ```

## 2. 切换

在旧服务器上执行：

```bash
# 1. 平滑停止该产品的全部 worker（SIGTERM），发布进程不停
# 2. 备份和恢复该账号的全部 stream
nats --context product_a backup account backup/product_a -f
nats --context product_a_new backup restore account backup/product_a
nats --context product_a_new stream info jobs | grep Messages      # 与旧服务器相同
# 3. 应用的 nats.url 改为新服务器，启动 worker，重启其他使用 NATS 的进程
nats --context product_a_new consumer report jobs
```

worker 与 HTTP 在同一进程（`start`）时，停 worker 期间改为只运行 `serve`。

## 3. 清理旧服务器

确认正常后立即执行，否则旧 stream 中的定时消息到时触发，误连旧服务器的 worker 会重复处理：

```bash
nats --context product_a stream ls                 # 逐个删除
nats --context product_a stream rm jobs -f
sudo rm -rf /opt/nats/backup/product_a
nats context rm product_a -f
```

再按 [删除产品](01-accounts.md#删除产品) 从 `nats.conf` 中删掉 `PRODUCT_A`。

## 回退

清理之前：`nats.url` 改回旧服务器，重启 worker。
