# 02 账号

每个产品一个账号。

| 方面 | 隔离 |
|---|---|
| subject、stream | 隔离。各账号都可用 stream 名 `jobs` |
| JetStream 资源 | 隔离，按账号限额，超额写入被拒绝 |
| 凭据 | 隔离 |
| CPU、磁盘 IO | **不隔离**。某个产品影响其他产品时，按 [04 迁移](04-migration.md) 迁出 |

同一产品的测试和正式环境用两个账号，例如 `product_a`、`product_a_test`。

## 生成密码和哈希

```bash
PASSWORD=$(openssl rand -base64 60 | tr -dc 'A-Za-z0-9' | head -c 40); echo "$PASSWORD"   # 存入密码管理器
PASSWORD="$PASSWORD" docker run --rm -e PASSWORD natsio/nats-box:latest nats server passwd    # 输出哈希
```

`nats.conf` 中只写哈希。明文交给应用（`NATS_URL`），不保存在服务器上。

## 新增产品

在 `nats.conf` 的 `accounts` 中加入：

```
  PRODUCT_C: {
    jetstream: { max_file: 5GB, max_mem: 64MB, max_streams: 20, max_consumers: 200 }
    users: [ { user: product_c, password: "<bcrypt 哈希>" } ]
  }
```

热加载：

```bash
docker compose kill -s HUP nats
docker compose logs --since 1m nats | grep -i reload    # 出现 Reloaded server configuration
```

## 修改配额

修改账号的 `jetstream: { max_file, max_mem }`，热加载。

- 各账号 `max_file` 之和不超过 `max_file_store`。
- 使用实时频道的产品，`max_mem` 要留出在线成员的空间。

## 删除产品

```bash
nb product_c stream rm jobs -f        # nb 见 03 运维
```

从 `nats.conf` 中删除该账号，热加载。

## 用量

```bash
curl -s '127.0.0.1:8222/jsz?accounts=true' | python3 -c 'import json,sys; [print(a["name"], "storage", a.get("storage"), "memory", a.get("memory")) for a in json.load(sys.stdin).get("account_details",[])]'
```

接入 OpenObserve 后看 `jetstream_account_storage_used`，见 [05](05-openobserve.md)。

账号超过十几个时改用 JWT 模式（`nsc` 管理）。
