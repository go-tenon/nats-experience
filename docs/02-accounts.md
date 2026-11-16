# 02 多产品账号

多个产品共用一台 NATS 时，每个产品一个账号。

## 账号隔离了什么

| 方面 | 效果 |
|---|---|
| subject 和 stream | 完全隔离：每个产品都可以用 tenon 默认的 stream 名 `jobs`；一个账号看不到、也删不了另一个账号的数据 |
| JetStream 资源 | 每个账号单独限额（磁盘、内存、stream 数、consumer 数），超出时写入被拒绝，不影响其他账号 |
| 凭据 | 每个产品自己的用户和密码，泄露或更换只影响它 |
| **CPU 和磁盘 IO** | **不隔离**：一个产品流量很大时，其他产品的延迟也会升高。这时按 [04 迁移](04-migration.md) 把它迁到单独的 NATS |

同一个产品的测试环境和正式环境建议分成两个账号，例如 `product_a`、`product_a_test`。

## 新增产品

把新的用户名加到参数中，重新运行安装脚本。**参数要列出全部产品**，没有列出的账号会从配置中删除：

```bash
bash scripts/install-nats.sh product_a product_b product_c
```

已有的密码不变，配置自动备份并热加载。新产品的密码：`sudo cat /etc/nats/product_c.password`。

## 修改配额

单个产品需要不同的配额时，直接编辑 `/etc/nats/nats.conf`：

```bash
sudo cp /etc/nats/nats.conf /etc/nats/nats.conf.bak
sudo vi /etc/nats/nats.conf       # 修改对应账号的 jetstream: { max_file: ..., max_mem: ... }
docker kill -s HUP nats
docker logs --since 1m nats | grep -i reload    # 看到 Reloaded server configuration 才算成功
```

注意：之后再运行 `install-nats.sh` 会按环境变量 `QUOTA_FILE`、`QUOTA_MEM` 重新生成所有账号的配额，手工修改会被覆盖。

- 所有账号的 `max_file` 之和不要超过 `max_file_store`（默认 30GB）。
- 用了实时频道的产品，`max_mem` 要给在线成员的 KV 留出空间。

## 删除产品

1. 删除它的数据：`nb product_c stream rm jobs -f`（`nb` 的定义见 [03 日常运维](03-operations.md#nats-命令行)）。
2. 去掉它的用户名重新运行 `install-nats.sh`，或者手工删掉配置中的账号后 `docker kill -s HUP nats`。
3. 删除密码文件：`sudo rm /etc/nats/product_c.password`。

## 查看各账号的用量

```bash
curl -s '127.0.0.1:8222/jsz?accounts=true' | python3 -c 'import json,sys; [print(a["name"], "storage", a.get("storage"), "memory", a.get("memory")) for a in json.load(sys.stdin).get("account_details",[])]'
nb sys server report jetstream       # 全局汇总
```

`nats server report accounts` 只列出当前有连接的账号，各账号的存储用量以 `/jsz?accounts=true` 为准。

## 账号很多以后

配置文件中的账号适合前期十几个以内的产品。再多时可以改用 NATS 的 JWT 模式（用 `nsc` 工具管理账号），增删账号不用改服务器配置，但运维更复杂。
