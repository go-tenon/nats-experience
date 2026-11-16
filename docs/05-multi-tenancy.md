# 05 多租户：多个产品共用一台 NATS

场景：产品前期流量不大，多个产品共用一台 NATS；某个产品流量起来后，再迁到单独的 NATS（见 [06](06-migration.md)）。

## 用账号隔离

NATS 的账号（account）是独立的命名空间。每个产品一个账号，产品的应用用自己账号下的用户连接。

| 方面 | 效果 |
|---|---|
| subject 和 stream | 完全隔离。两个产品都可以用 tenon 默认的 stream 名 `jobs`，互不冲突；一个账号看不到、也删不了另一个账号的 stream |
| JetStream 资源 | 每个账号单独限额：磁盘、内存、stream 数、consumer 数。超出时写入被拒绝，不影响其他账号 |
| 凭据 | 每个产品自己的用户和密码，泄露或换密码只影响它 |
| 用量 | 可以按账号查看连接、消息量、存储（`/jsz?accounts=true`） |
| **CPU 和磁盘 IO** | **不隔离**：一个产品流量很大时，其他产品的延迟也会升高。这是该把它迁出去的信号 |

另外，单机挂了所有产品同时受影响（任务暂停处理）。

## 配置

见 `config/nats.conf.example`，或用 `scripts/ecs/install-nats.sh` 生成：

```
accounts {
  PRODUCT_A: {
    jetstream: { max_file: 5GB, max_mem: 64MB, max_streams: 20, max_consumers: 200 }
    users: [ { user: product_a, password: "<bcrypt 哈希>" } ]
  }
  SYS: { users: [ { user: sys, password: "<bcrypt 哈希>" } ] }   # 运维账号
}
system_account: SYS
```

- 定义了 `accounts` 之后，用户都要写在账号中，不能再用顶层的 `authorization`。
- 增删账号、改配额：改配置后 `docker kill -s HUP nats`，不用重启（实测 `Reloaded: accounts`）。
- 所有账号的 `max_file` 之和不要超过 `max_file_store`。
- tenon 的实时功能（在线成员）用内存存储的 KV，`max_mem` 要给它留出来。
- 同一个产品的测试环境和正式环境建议分成两个账号。
- 产品多了以后可以改用 JWT 模式（`nsc` 管理），增删账号不用改服务器配置，但运维更复杂，前期没必要。

## 验证结果

实验机上（`results/ecs/06-accounts.txt`）：

- `product_a` 和 `product_b` 各自看到自己的 `jobs` stream；A 中有 2 条定时消息时，B 的为空。
- B 订阅 `jobs.>`，A 向 `jobs.test` 发 5 条，B 收到 0 条（对照：A 自己订阅收到 5 条）。
- B 的磁盘配额设为 64MB，写入 100MB 时停在 67108830 字节，超出的 36638 条被拒绝。
- `sys` 用户用 `nats server report jetstream` 看到全局汇总；`nats server report accounts` 只列出当前有连接的账号，各账号的存储用量用 `/jsz?accounts=true` 查。

## 注意

- **不要往 `<stream>.>`（默认 `jobs.>`）发其他消息。** tenon 的队列 stream 收录这个范围内的所有 subject，发进去的消息都会被存进队列的 stream（隔离测试时发到 `jobs.test` 的消息就被存了进去，还随备份迁移了）。
- 加新产品：把新用户名加到 `scripts/ecs/install-nats.sh` 的参数中重新运行（参数要列出全部产品），已有的密码不变，配置自动备份并热加载。
