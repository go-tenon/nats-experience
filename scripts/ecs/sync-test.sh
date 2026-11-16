# JetStream 磁盘同步策略的代价：默认（2 分钟）、sync_interval: 1s、sync_interval: always。
# 需要 ~/mqbench（tools/mqbench 编译的 linux 版本）。客户端和服务端在同一台机器上，只比较相对差异。
mk() {
  docker rm -f bnats >/dev/null 2>&1; docker volume rm -f bnats >/dev/null 2>&1
  conf=$HOME/bnats-$1.conf
  case $1 in
    default) js='jetstream { store_dir: /data }' ;;
    1s)      js='jetstream { store_dir: /data, sync_interval: "1s" }' ;;
    always)  js='jetstream { store_dir: /data, sync_interval: always }' ;;
  esac
  printf 'port: 14222\nhttp: 127.0.0.1:18222\n%s\n' "$js" > "$conf"
  docker run -d --name bnats --network host -v bnats:/data -v "$conf":/etc/nats.conf:ro nats:2.15-alpine nats-server -c /etc/nats.conf >/dev/null
  until curl -sf 127.0.0.1:18222/healthz >/dev/null; do sleep 0.2; done
}
M="$HOME/mqbench -sys js -nats nats://127.0.0.1:14222"
r() { grep -oE "rate=[0-9]+|p50=[0-9.]+ms|p99=[0-9.]+ms" | tr "\n" " "; }
# always 很慢（约 450 条/秒），排空 30 万条要十几分钟；可以只测 default 和 1s
for s in ${MODES:-default 1s always}; do
  mk $s; a=$($M -mode pub -n 100000 -c 16 | r)
  mk $s; b=$($M -mode pub -n 200000 -c 64 | r)
  mk $s; c=$($M -mode pub -n 300000 -c 4 -async 1000 | r); d=$($M -mode sub -n 300004 -c 16 -setup=false | r)
  mk $s; e=$($M -mode e2e -rate 1000 -dur 30s -c 32 | grep RESULT | r)
  mk $s; f=$($M -mode e2e -rate 3000 -dur 30s -c 32 | grep RESULT | r)
  echo "sync=$s | c16: $a| c64: $b| async: $c| drain: $d| e2e1k: $e| e2e3k: $f"
done
docker rm -f bnats >/dev/null; docker volume rm -f bnats >/dev/null
