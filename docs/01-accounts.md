# 01 账号

每个项目一个账号。账号之间 subject、stream、凭据隔离；磁盘、CPU、IO 共用，某个账号影响其他账号时按 [03 迁移](03-migration.md) 迁出。同一项目的测试和正式环境用两个账号。

账号和用户的写法见 README 中的 [config/nats.conf](../README.md#confignatsconf)，修改后执行 `make reload`。命令都在 `nats` 目录中执行。

## 用户

一个账号下的多个应用各用一个用户，便于单独吊销。

吊销（凭据泄露、停用）：删掉这个用户，`make reload`。使用它的连接立即断开，重连被拒绝。换密码同理：改密码、`make reload`，再更新应用的配置。

## 删除账号

```bash
nats --context c stream ls                    # 逐个删除，例如：
nats --context c stream rm jobs -f
```

从 `config/nats.conf` 中删掉这个账号，`make reload`，然后删除它的数据目录和 context：

```bash
sudo rm -rf data/jetstream/C
nats context rm c -f
```

`nats` 命令的用法见 [02 运维](02-operations.md#命令行)。

## 查看

```bash
nats --context sys server report accounts
nats --context sys server report jetstream
```
