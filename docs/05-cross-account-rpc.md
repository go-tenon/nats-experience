# 05 跨账号 RPC

产品 B、C 调用产品 A 的 natsrpc 端点。以 A 的前缀 `producta.v1` 为例。

NATS 的导出、导入决定请求能否送到 A；natsrpc 的 token 和端点权限决定能否调用，调用方仍需 A 签发的 API token（`natsrpc.Token`）。

## 同一台 NATS：导出和导入

在 `config/nats.conf` 的 `accounts` 中，A 导出一次，每个调用方导入，然后[重新加载](02-operations.md#修改配置)：

```
  PRODUCT_A {
    jetstream: enabled
    users = [ ... ]
    exports = [
      { service: "producta.v1.>" }
    ]
  }

  PRODUCT_B {
    jetstream: enabled
    users = [ ... ]
    imports = [
      { service: { account: PRODUCT_A, subject: "producta.v1.>" } }
    ]
  }
```

调用方用自己的连接调用，代码不变：

```go
out, err := natsrpc.Call[ArticleDTO](ctx, nc, "producta.v1.articles.get", in, natsrpc.Token(token))
```

- 未导出的 subject，调用时返回 `natsrpc.ErrNoResponders`。
- 取消：删掉调用方的 `imports` 中的这一项，重新加载。
- 导出和导入只在同一台 NATS 内有效。

## 不同的 NATS：调用方直连 A

A 迁到单独的 NATS 后，导入失效。在 A 的 NATS 上，给 A 的账号为每个调用方加一个只能调用 RPC 的用户，重新加载：

```
    users = [
      { user: product_a, password: "<密码>" }
      {
        user: rpc_productb, password: "<密码>"
        permissions: {
          publish: "producta.v1.>"
          subscribe: "_INBOX_productb.>"
        }
      }
    ]
```

每个调用方的收件箱前缀专用且各不相同（C 用 `rpc_productc` 和 `_INBOX_productc`）。允许订阅 `_INBOX.>` 或共用前缀时，调用方能收到 A 账号内其他请求的回复。

调用方的应用另建一条连接：

```go
nca, err := nats.Connect("nats://rpc_productb:<密码>@<A 的地址>:4222",
    nats.CustomInboxPrefix("_INBOX_productb"),
    nats.MaxReconnects(-1))
out, err := natsrpc.Call[ArticleDTO](ctx, nca, "producta.v1.articles.get", in, natsrpc.Token(token))
```

`natsx.Connect` 不能设置收件箱前缀，这条连接直接用 nats.go 建立。

该用户只能调用 `producta.v1.*`；A 的其他 subject、默认 `_INBOX` 前缀、订阅 `_INBOX.>`、A 的 JetStream 均被拒绝。吊销时删掉这个用户，重新加载。
