# 04 迁移

把产品 `PRODUCT_A` 迁到单独的 NATS。新服务器使用同一个运营者，应用的 `.creds` 不变，只改地址。

worker 停止期间发布照常进行，新任务留在 PostgreSQL 中，worker 连上新 NATS 后自动转发；NATS 中已有的任务、定时消息和 consumer 进度通过备份恢复迁移。任务暂停处理的时长等于 worker 停止的时长。

命令在旧服务器上以管理员用户执行。

## 1. 准备新服务器

1. 按 [01 部署](01-deploy.md) 安装 `nats-server`、创建用户和目录、安装服务，不安装 nsc。
2. 从旧服务器复制 `/etc/nats/nats.conf`、`/etc/nats/resolver.conf`，修改 `server_name`，启动服务。
3. 安全组放行应用服务器和旧服务器访问新服务器的 4222。
4. 推送账号：

   ```bash
   nsc push --account PRODUCT_A --account-jwt-server-url nats://<新地址>:4222
   nats context add product_a_new --server nats://<新地址>:4222 --creds ~/.local/share/nats/nsc/keys/creds/tenon/PRODUCT_A/app.creds
   ```

## 2. 切换

```bash
# 1. 平滑停止该产品的全部 worker（SIGTERM），发布进程不停
# 2. 备份和恢复
nats --context product_a backup stream jobs ~/natsbackup/jobs --no-progress
nats --context product_a_new backup restore stream ~/natsbackup/jobs --no-progress
nats --context product_a_new stream info jobs | grep -i messages     # 与旧服务器相同
# 3. 应用的 nats.url 改为新服务器，启动 worker，重启其他使用 NATS 的进程
nats --context product_a_new consumer report jobs
```

worker 与 HTTP 在同一进程（`start`）时，停 worker 期间改为只运行 `serve`。

## 3. 清理旧服务器

确认正常后立即执行，否则旧 stream 中的定时消息到时触发，误连旧服务器的 worker 会重复处理：

```bash
nats --context product_a stream rm jobs -f
nsc push --account-removal PRODUCT_A
rm -rf ~/natsbackup
nats context rm product_a -f
```

此后 `PRODUCT_A` 的推送加上 `--account-jwt-server-url nats://<新地址>:4222`。

## 回退

清理之前：`nats.url` 改回旧服务器，重启 worker。
