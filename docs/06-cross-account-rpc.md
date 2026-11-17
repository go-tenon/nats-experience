# 06 跨账号 RPC

产品 B、C 调用产品 A 的 natsrpc 端点。以 A 的前缀 `producta.v1` 为例。

NATS 的导出、导入决定请求能否送到 A；natsrpc 的 token 和端点权限决定能否调用，调用方仍需 A 签发的 API token（`natsrpc.Token`）。

## 同一台 NATS：导出和导入

A 导出一次，每个调用方导入：

```bash
nsc add export --account PRODUCT_A --service --subject "producta.v1.>" --name producta-rpc
nsc add import --account PRODUCT_B --service --src-account PRODUCT_A --remote-subject "producta.v1.>" --name producta-rpc
nsc add import --account PRODUCT_C --service --src-account PRODUCT_A --remote-subject "producta.v1.>" --name producta-rpc
nsc push --all
```

调用方用自己的连接调用，代码不变：

```go
out, err := natsrpc.Call[ArticleDTO](ctx, nc, "producta.v1.articles.get", in, natsrpc.Token(token))
```

- 未导出的 subject，调用时返回 `natsrpc.ErrNoResponders`。
- 取消：`nsc delete import --account PRODUCT_B --subject "producta.v1.>"`，推送。
- 导出和导入只在同一台 NATS 内有效。

## 不同的 NATS：调用方直连 A

A 迁到单独的 NATS 后，导入失效。在 A 的账号下为每个调用方建一个用户，调用方另建一条连接：

```bash
nsc add user --account PRODUCT_A --name rpc-productb \
  --allow-pub "producta.v1.>" --allow-sub "_INBOX_productb.>"
nsc push --account PRODUCT_A --account-jwt-server-url nats://<A 的地址>:4222
# 凭据：~/.local/share/nats/nsc/keys/creds/tenon/PRODUCT_A/rpc-productb.creds
```

每个调用方的收件箱前缀专用且各不相同（C 用 `rpc-productc` 和 `_INBOX_productc`）。允许订阅 `_INBOX.>` 或共用前缀时，调用方能收到 A 账号内其他请求的回复。

调用方的应用：

```go
nca, err := nats.Connect("nats://<A 的地址>:4222",
    nats.UserCredentials("/etc/app/producta-rpc.creds"),
    nats.CustomInboxPrefix("_INBOX_productb"),
    nats.MaxReconnects(-1))
out, err := natsrpc.Call[ArticleDTO](ctx, nca, "producta.v1.articles.get", in, natsrpc.Token(token))
```

`natsx.Connect` 不能设置收件箱前缀，这条连接直接用 nats.go 建立。

该用户只能调用 `producta.v1.*`；A 的其他 subject、默认 `_INBOX` 前缀、订阅 `_INBOX.>`、A 的 JetStream 均被拒绝。

删除该用户前先吊销，见 [02 账号](02-accounts.md#用户)。
