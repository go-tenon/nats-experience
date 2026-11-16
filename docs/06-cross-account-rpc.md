# 06 跨账号 RPC

产品 B 调用产品 A 的 natsrpc 端点。以 A 的前缀 `producta.v1` 为例。

两层控制：

- NATS 账号的导出、导入决定请求能否送到 A；
- natsrpc 的 token 和端点权限决定能否调用。B 仍需 A 签发的 API token（`natsrpc.Token`）。

## 同一台 NATS：导出和导入

A 私有导出，为 B 签发授权；B 导入：

```bash
nsc add export --account PRODUCT_A --service --private --subject "producta.v1.>" --name producta-rpc
nsc generate activation --account PRODUCT_A --subject "producta.v1.>" --target-account PRODUCT_B --output-file b.tok   # 可加 --expiry
nsc add import --account PRODUCT_B --service --token b.tok --name producta-rpc
nsc push --account PRODUCT_A && nsc push --account PRODUCT_B
rm b.tok
```

B 用自己的连接调用，代码不变：

```go
out, err := natsrpc.Call[ArticleDTO](ctx, nc, "producta.v1.articles.get", in, natsrpc.Token(token))
```

- 只导出需要的前缀。未导出的 subject，B 调用时返回 `natsrpc.ErrNoResponders`。
- 取消：`nsc delete import --account PRODUCT_B --subject "producta.v1.>"`，必要时删除 A 的导出，推送两个账号。
- 导出和导入只在同一台 NATS（或同一集群）内有效。

## 不同的 NATS：B 直连 A

A 迁到单独的 NATS 后，导入失效。在 A 的账号下为 B 建专用用户，B 的应用另建一条连接：

```bash
nsc add user --account PRODUCT_A --name rpc-productb \
  --allow-pub "producta.v1.>" --allow-sub "_INBOX_productb.>"
nsc push --account PRODUCT_A --account-jwt-server-url nats://<A 的地址>:4222
# 凭据：~/.local/share/nats/nsc/keys/creds/tenon/PRODUCT_A/rpc-productb.creds
```

收件箱前缀必须专用。允许订阅 `_INBOX.>` 时，该用户能收到 A 账号内其他请求的回复，包括 A 的 JetStream API 回复。

B 的应用：

```go
nca, err := nats.Connect("nats://<A 的地址>:4222",
    nats.UserCredentials("/etc/app/producta-rpc.creds"),
    nats.CustomInboxPrefix("_INBOX_productb"),
    nats.MaxReconnects(-1))
out, err := natsrpc.Call[ArticleDTO](ctx, nca, "producta.v1.articles.get", in, natsrpc.Token(token))
```

`natsx.Connect` 不能设置收件箱前缀，这条连接直接用 nats.go 建立。

该用户的权限：

| 操作 | 结果 |
|---|---|
| 调用 `producta.v1.*` | 允许 |
| 调用 A 的其他 subject | 拒绝 |
| 默认 `_INBOX` 前缀的请求 | 拒绝（收不到回复） |
| 订阅 `_INBOX.>` | 拒绝 |
| 访问 A 的 JetStream | 拒绝 |

## 撤销

**删除用户不会使其凭据失效**，服务器只校验签名。先吊销再删除：

```bash
nsc revocations add-user --account PRODUCT_A --name rpc-productb
nsc push --account PRODUCT_A
nsc delete user --account PRODUCT_A --name rpc-productb
```

已经删除的用户按公钥吊销：`nsc revocations add-user --account PRODUCT_A --user-public-key <U...>`。公钥在凭据文件的 JWT 中（`sub` 字段）。
