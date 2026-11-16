# 02 账号

每个产品一个账号，账号下的用户持有 `.creds` 连接。命令在部署目录中通过 `nb` 执行（见 [01 部署](01-deploy.md#安装)）。

| 方面 | 隔离 |
|---|---|
| subject、stream | 隔离。各账号都可用 stream 名 `jobs` |
| 凭据 | 隔离 |
| 磁盘、CPU、IO | **不隔离**。账号不设配额，共用 `max_file_store`。某个产品影响其他产品时，按 [04 迁移](04-migration.md) 迁出 |

同一产品的测试和正式环境用两个账号，例如 `PRODUCT_A`、`PRODUCT_A_TEST`。

## 新增产品

```bash
A=PRODUCT_C
nb nsc add account --name $A
nb nsc edit account --name $A --sk generate --js-disk-storage -1 --js-mem-storage -1 --js-streams -1 --js-consumer -1
nb nsc add user --account $A --name app
nb nsc push --account $A
```

- `--js-*` 开启 JetStream 且不限额。不设置时账号的 JetStream 是关闭的。
- `--sk generate` 生成账号签名密钥，用户由它签发。
- 凭据：`admin/nkeys/creds/tenon/$A/app.creds`，复制到应用服务器（权限 600），配置为 `nats.credentials`。

为运维添加 context：

```bash
nb nats context add product_c --server nats://127.0.0.1:4222 --creds /nsc/nkeys/creds/tenon/$A/app.creds
nb nats --context product_c account info
```

## 多个用户

一个产品的多个应用或环境各用一个用户，便于单独吊销：

```bash
nb nsc add user --account PRODUCT_A --name worker
nb nsc push --account PRODUCT_A
```

## 吊销和重新签发

凭据泄露或人员变动时：

```bash
nb nsc add user --account PRODUCT_A --name app2                  # 新凭据，更新到应用
nb nsc revocations add-user --account PRODUCT_A --name app       # 吊销旧凭据
nb nsc push --account PRODUCT_A
```

推送后旧凭据立即无法连接。撤销吊销：`nb nsc revocations delete-user --account PRODUCT_A --name app`，再推送。

## 删除产品

```bash
nb nats --context product_c stream rm jobs -f
nb nsc push --account-removal PRODUCT_C                          # 从服务器删除
nb nsc delete account --name PRODUCT_C --rm-nkey --rm-creds      # 从本地删除
nb nats context rm product_c -f
```

服务器上的账号 JWT 改名为 `.deleted`，可以恢复。

## 查看

```bash
nb nsc list accounts
nb nsc describe account --name PRODUCT_A
nb nsc list users --account PRODUCT_A
curl -s '127.0.0.1:8222/jsz?accounts=true' | python3 -c 'import json,sys; [print(a["name"], "storage", a.get("storage"), "memory", a.get("memory")) for a in json.load(sys.stdin).get("account_details",[])]'
```

`/jsz` 中的账号名是公钥，对应关系见 `nsc list accounts`。
