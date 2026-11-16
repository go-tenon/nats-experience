# broker 和 worker 同时 kill -9：检查重启后正在处理的任务是否重新投递。
# 先发布 3000 个编号任务，worker 以 200 并发、每个任务 5 秒处理；20 秒后同时 kill -9 worker 和 broker，
# 重启 broker，再用新的 worker 处理剩下的任务，最后核对每个编号是否都处理过。
# 只 kill broker 的测试测不出在途消息的丢失：worker 仍会把手上的任务处理完。
source "$(dirname "$0")/lib.sh"
for sys in nsq js; do for i in 1 2; do
  start_$sys; L=$S/proc-$sys-$i.log; rm -f "$L"
  echo "== joint crash sys=$sys run=$i (5s/任务, 200 并发)"
  $B -sys $sys -mode pubids -n 3000 -inflight 200 | grep PROG
  $B -sys $sys -mode worker -setup=false -log "$L" -c 200 -inflight 200 -work 5s -wait 15s 2>/dev/null & p=$!
  sleep 20; kill -9 $p; docker kill -s KILL mqb-$sys >/dev/null; wait $p 2>/dev/null
  echo "processed before crash: $(( $(stat -c %s "$L")/8 ))"
  sleep 2; docker start mqb-$sys >/dev/null; sleep 2
  $B -sys $sys -mode worker -setup=false -log "$L" -c 200 -inflight 200 -work 1ms -wait 15s 2>/dev/null
  $B -sys $sys -mode check -log "$L" -n 3000
done; done
docker rm -f mqb-nsq mqb-js >/dev/null
