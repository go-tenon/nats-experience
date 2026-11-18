# 01 账号

每个产品一个账号。账号之间 subject、stream、凭据隔离；磁盘、CPU、IO 共用，某个账号影响其他账号时按 [03 迁移](03-migration.md) 迁出。同一产品的测试和正式环境用两个账号。

命令都在 `nats` 目录中执行。账号和用户都写在 `config/nats.conf` 的 `accounts` 中。修改后检查语法，再通知 NATS 重新加载：

```bash
sudo docker compose exec nats nats-server -t -c /etc/nats/nats.conf   # configuration file ... is valid
sudo docker compose kill -s HUP nats
sudo docker compose logs --tail 3 nats                               # Reloaded: accounts
```

重新加载不重启服务器，已有的连接不断开。配置有错时，日志中出现 `Failed to reload server configuration`，服务器继续使用原来的配置。

## 新增账号

在 `accounts` 中加一段，密码用 `openssl rand -hex 24` 生成：

```
  C {
    jetstream: enabled
    users = [
      { user: c, password: "<密码>" }
    ]
  }
```

- `jetstream: enabled` 开启 JetStream，不限额；不写时关闭。
- 用户名在所有账号中唯一。

重新加载后，应用按 [应用配置](../README.md#应用配置) 连接。

## 用户

一个账号下的多个应用各用一个用户，便于单独吊销：

```
    users = [
      { user: a, password: "<密码>" }
      { user: a_worker, password: "<密码>" }
    ]
```

吊销（凭据泄露、停用）：删掉这个用户，重新加载。使用它的连接立即断开，重连被拒绝。换密码同理：改密码、重新加载，再更新应用的配置。

## 删除账号

```bash
nats --context c stream ls                    # 逐个删除，例如：
nats --context c stream rm jobs -f
```

从 `config/nats.conf` 中删掉这一段，重新加载，然后删除它的数据目录和 context：

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
