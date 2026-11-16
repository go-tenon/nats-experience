# 迁移演练：把产品 A（用户 product_a）从旧 NATS（:4222）搬到新 NATS（:4223），期间应用持续发布任务。
# 准备：
#   - 旧 NATS 由 install-nats.sh 部署，有 product_a 账号；新 NATS 用同样的用户和密码监听 4223（见 docs/06-migration.md）
#   - PostgreSQL 在 127.0.0.1:5432，库 proda，已执行 ./tenonmig -mode setup
#   - ~/tenonmig 是 tools/tenonmig 编译的 linux 版本
IP=${PRIVATE_IP:-$(ip -4 -o addr show eth0 | awk '{print $4}' | cut -d/ -f1)}
DSN="postgres://postgres:postgres@127.0.0.1:5432/proda?sslmode=disable"
url() { echo "nats://product_a:$(sudo cat /etc/nats/product_a.password)@$IP:$1"; }
nb() { p=$1; shift; docker run --rm --network host -v $HOME/natsbackup:/backup -e NATS_URL=nats://$IP:$p -e NATS_USER=product_a -e NATS_PASSWORD="$(sudo cat /etc/nats/product_a.password)" natsio/nats-box:latest nats "$@"; }
msgs() { nb $1 stream info jobs -j 2>/dev/null | grep -oE '"messages": [0-9]+' | head -1 | grep -oE '[0-9]+'; }
ts() { date +%H:%M:%S; }
mkdir -p $HOME/natsbackup; chmod 777 $HOME/natsbackup
echo "$(ts) 1. worker 连旧服务器（4222），发布方开始：50 个/秒，1/10 延迟 2-3 分钟"
NATS_URL=$(url 4222) ~/tenonmig -mode work -dsn "$DSN" > work-old.log 2>&1 & W1=$!
~/tenonmig -mode pub -rate 50 -delayed 10 -dsn "$DSN" > pub.log 2>&1 & P=$!
sleep 60
echo "$(ts) 2. 平滑停止 worker（发布方继续）"; kill -TERM $W1; wait $W1; tail -1 work-old.log
sleep 5
echo "$(ts)    旧服务器 A 的 jobs：$(msgs 4222) 条消息（等待处理 + 定时）"
echo "$(ts) 3. 备份并恢复到新服务器（4223）"
nb 4222 backup stream jobs /backup/jobs-a --no-progress 2>&1 | grep -vi warning | tail -2
nb 4223 backup restore stream /backup/jobs-a --no-progress 2>&1 | grep -vi warning | tail -2
echo "$(ts)    新服务器 A 的 jobs：$(msgs 4223) 条消息；consumer：$(nb 4223 consumer ls jobs -n 2>/dev/null | tr '\n' ' ')"
echo "$(ts) 4. worker 改连新服务器"
NATS_URL=$(url 4223) ~/tenonmig -mode work -dsn "$DSN" > work-new.log 2>&1 & W2=$!
sleep 30
echo "$(ts) 5. 停止发布方"; kill -TERM $P; wait $P; cat pub.log
for i in $(seq 1 25); do
  sleep 10; c=$(~/tenonmig -mode check -dsn "$DSN" 2>/dev/null)
  echo "$(ts)    $c"
  echo "$c" | grep -q "not_due_yet=0 " && break
done
sleep 20
echo "$(ts) 最终核对：$(~/tenonmig -mode check -dsn "$DSN" 2>/dev/null)"
echo "$(ts)    新服务器 A 的 jobs 剩余：$(msgs 4223)；旧服务器 A 的 jobs 剩余：$(msgs 4222)（切换后应立即删除）"
kill -TERM $W2; wait $W2
