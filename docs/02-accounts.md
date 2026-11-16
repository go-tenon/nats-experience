# 02 账号

每个产品一个账号。

| 方面 | 隔离 |
|---|---|
| subject、stream | 隔离。各账号都可用 stream 名 `jobs` |
| JetStream 资源 | 隔离，按账号限额，超额写入被拒绝 |
| 凭据 | 隔离 |
| CPU、磁盘 IO | **不隔离**。某个产品影响其他产品时，按 [04 迁移](04-migration.md) 迁出 |

同一产品的测试和正式环境用两个账号，例如 `product_a`、`product_a_test`。

## 新增产品

参数列出全部产品，未列出的账号会被删除：

```bash
bash scripts/install-nats.sh product_a product_b product_c
sudo cat /etc/nats/product_c.password
```

## 修改配额

```bash
sudo cp /etc/nats/nats.conf /etc/nats/nats.conf.bak
sudo vi /etc/nats/nats.conf       # 修改账号的 jetstream: { max_file, max_mem }
docker kill -s HUP nats
docker logs --since 1m nats | grep -i reload    # 出现 Reloaded server configuration
```

再次运行 `install-nats.sh` 会按 `QUOTA_FILE`、`QUOTA_MEM` 覆盖所有配额。

- 各账号 `max_file` 之和不超过 `max_file_store`。
- 使用实时频道的产品，`max_mem` 要留出在线成员的空间。

## 删除产品

```bash
nb product_c stream rm jobs -f                        # nb 见 03 运维
bash scripts/install-nats.sh product_a product_b      # 不再列出 product_c
sudo rm /etc/nats/product_c.password
```

## 用量

```bash
curl -s '127.0.0.1:8222/jsz?accounts=true' | python3 -c 'import json,sys; [print(a["name"], "storage", a.get("storage"), "memory", a.get("memory")) for a in json.load(sys.stdin).get("account_details",[])]'
nb sys server report jetstream
```

`nats server report accounts` 只列出有连接的账号。

账号超过十几个时改用 JWT 模式（`nsc` 管理）。
