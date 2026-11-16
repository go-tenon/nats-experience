# 02 账号

每个产品一个账号，账号下的用户持有 `.creds` 连接。命令以管理员用户执行。

| 方面 | 隔离 |
|---|---|
| subject、stream | 隔离。各账号都可用 stream 名 `jobs` |
| 凭据 | 隔离 |
| 磁盘、CPU、IO | **不隔离**。账号不设配额，共用 `max_file_store`。某个产品影响其他产品时，按 [04 迁移](04-migration.md) 迁出 |

同一产品的测试和正式环境用两个账号，例如 `PRODUCT_A`、`PRODUCT_A_TEST`。

## 新增产品

```bash
A=PRODUCT_C
nsc add account --name $A
nsc edit account --name $A --sk generate --js-disk-storage -1 --js-mem-storage -1 --js-streams -1 --js-consumer -1
nsc add user --account $A --name app
nsc push --account $A
nats context add product_c --server nats://127.0.0.1:4222 --creds ~/.local/share/nats/nsc/keys/creds/tenon/$A/app.creds
nats --context product_c account info
```

- `--js-*` 开启 JetStream 且不限额。不设置时账号的 JetStream 是关闭的。
- `--sk generate` 生成账号签名密钥，用户由它签发。
- 凭据 `~/.local/share/nats/nsc/keys/creds/tenon/$A/app.creds` 复制到应用服务器（权限 600），配置为 `nats.credentials`。

## 多个用户

一个产品的多个应用或环境各用一个用户，便于单独吊销：

```bash
nsc add user --account PRODUCT_A --name worker
nsc push --account PRODUCT_A
```

## 吊销和重新签发

```bash
nsc add user --account PRODUCT_A --name app2                  # 新凭据，更新到应用
nsc revocations add-user --account PRODUCT_A --name app       # 吊销旧凭据
nsc push --account PRODUCT_A
```

推送后旧凭据立即无法连接。撤销吊销：`nsc revocations delete-user --account PRODUCT_A --name app`，再推送。

**删除用户不会使其凭据失效**，服务器只校验签名。删除用户前先吊销：

```bash
nsc revocations add-user --account PRODUCT_A --name app
nsc push --account PRODUCT_A
nsc delete user --account PRODUCT_A --name app
```

## 删除产品

```bash
nats --context product_c stream rm jobs -f
nsc push --account-removal PRODUCT_C                          # 从服务器删除
nsc delete account --name PRODUCT_C --rm-nkey --rm-creds      # 从本地删除
nats context rm product_c -f
```

服务器上的账号 JWT 改名为 `.deleted`，可以恢复。

## 查看

```bash
nsc list accounts
nsc describe account --name PRODUCT_A
nsc list users --account PRODUCT_A
curl -s '127.0.0.1:8222/jsz?accounts=true' | python3 -c 'import json,sys; [print(a["name"], "storage", a.get("storage"), "memory", a.get("memory")) for a in json.load(sys.stdin).get("account_details",[])]'
```

`/jsz` 中的账号名是公钥，对应关系见 `nsc list accounts`。
