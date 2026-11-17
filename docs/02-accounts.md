# 02 账号

每个产品一个账号。账号之间 subject、stream、凭据隔离；磁盘、CPU、IO 共用，某个产品影响其他产品时按 [04 迁移](04-migration.md) 迁出。同一产品的测试和正式环境用两个账号。

## 新增产品

```bash
A=PRODUCT_C
nsc add account --name $A
nsc edit account --name $A --js-enable 0        # 开启 JetStream，不限额；不设置时关闭
nsc add user --account $A --name app
nsc push --account $A
nats context add product_c --server nats://127.0.0.1:4222 --creds ~/.local/share/nats/nsc/keys/creds/tenon/$A/app.creds
```

把 `~/.local/share/nats/nsc/keys/creds/tenon/$A/app.creds` 复制到应用服务器（权限 600），配置为 `nats.credentials`。

## 用户

一个产品的多个应用各用一个用户，便于单独吊销：

```bash
nsc add user --account PRODUCT_A --name worker
nsc push --account PRODUCT_A
```

吊销（凭据泄露、停用）：

```bash
nsc revocations add-user --account PRODUCT_A --name worker
nsc push --account PRODUCT_A
```

推送后凭据立即失效。删除用户不会使凭据失效，删除前先吊销。

## 删除产品

```bash
nats --context product_c stream rm jobs -f
nsc push --account-removal PRODUCT_C
nsc delete account --name PRODUCT_C --rm-nkey --rm-creds
nats context rm product_c -f
```

## 查看

```bash
nsc list accounts
nsc list users --account PRODUCT_A
nats --context sys server report jetstream
```
