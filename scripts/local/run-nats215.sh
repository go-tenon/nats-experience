export NATS_IMAGE=nats:2.15-alpine
source "$(dirname "$0")/lib.sh"
docker run --rm $NATS_IMAGE nats-server --version
run() { local sys=$1; shift; start_$sys; local m=$(mon mqb-$sys $S/mon.tmp); $B -sys $sys "$@" | grep RESULT; kill $m; wait $m 2>/dev/null; summ $S/mon.tmp; }
echo "######## A: 吞吐"
for sys in nsq js; do
  for c in 1 16 64; do n=$((c==1?50000:300000)); echo "== $sys sync pub c=$c"; run $sys -mode pub -n $n -c $c; done
  echo "== $sys sync pub 128B c=64"; run $sys -mode pub -n 300000 -c 64 -size 128
  echo "== $sys async pub + drain"; start_$sys
  m=$(mon mqb-$sys $S/mon.tmp); $B -sys $sys -mode pub -n 1000000 -c 4 -async 1000 | grep RESULT; kill $m; summ $S/mon.tmp
  m=$(mon mqb-$sys $S/mon.tmp); $B -sys $sys -mode sub -n 1000004 -c 16 -setup=false | grep RESULT; kill $m; summ $S/mon.tmp
done
echo "######## B: 固定速率端到端延迟"
for sys in nsq js; do for r in 1000 5000 10000 20000; do echo "== $sys rate=$r"; run $sys -mode e2e -rate $r -dur 45s -c 64; done; done
echo "######## C1: 崩溃与重启（只有 broker 挂）"
for kind in kill restart; do
  start_js; echo "== js crash kind=$kind"
  $B -sys js -mode crash -n 200000 -rate 5000 -c 16 -inflight 200 -work 2ms -wait 60s > $S/crash.out 2>&1 & p=$!
  sleep 15
  if [ $kind = kill ]; then docker kill -s KILL mqb-js >/dev/null; sleep 3; docker start mqb-js >/dev/null; else docker restart -t 10 mqb-js >/dev/null; fi
  wait $p; grep RESULT $S/crash.out
done
echo "######## C2: broker 和 worker 同时 kill -9（5s 任务，200 并发）"
for i in 1 2; do
  start_js; L=$S/proc-js215-$i.log; rm -f $L
  $B -sys js -mode pubids -n 3000 -inflight 200 | grep PROG
  $B -sys js -mode worker -setup=false -log $L -c 200 -inflight 200 -work 5s -wait 15s 2>/dev/null & p=$!
  sleep 20; kill -9 $p; docker kill -s KILL mqb-js >/dev/null; wait $p 2>/dev/null
  echo "processed before crash: $(( $(stat -c %s $L)/8 ))"
  sleep 2; docker start mqb-js >/dev/null; sleep 2
  $B -sys js -mode worker -setup=false -log $L -c 200 -inflight 200 -work 1ms -wait 15s 2>/dev/null
  $B -sys js -mode check -log $L -n 3000
done
echo "######## D: 延迟重投"
echo "== MaxAckPending=1000 时延迟 NAK 是否卡住（5000 条，延迟 60s，最多等 70s）"
start_js; $B -sys js -mode defer -n 5000 -inflight 1000 -c 32 -delay 60s -wait 20s 2>&1 | grep -E 'DEFERRED_ALL|RESULT' | head -3
MAXACK=250000 bash "$(dirname "$0")/defer.sh" js 200000 1000 kill
MAXACK=700000 bash "$(dirname "$0")/defer.sh" js 600000 1000 none
echo "######## E: 长时间运行与积压"
c=mqb-js; start_js; m=$(mon $c $S/mon-soak-js215.tmp)
echo "== soak js 10k/s 10min"
$B -sys js -mode e2e -rate 10000 -dur 600s -c 64 | grep -E 'RESULT|WIN' > $S/soak215.win; grep RESULT $S/soak215.win
kill $m; summ $S/mon-soak-js215.tmp
awk '/^WIN/{for(i=1;i<=NF;i++) if($i~/^p99=/){v=substr($i,5)+0; if(!n||v<mn)mn=v; if(v>mx)mx=v; n=1}} END{print "window p99 min/max(ms):", mn, mx}' $S/soak215.win
echo "mem 30s / end (cpu anon cur oom): $(sed -n '30p' $S/mon-soak-js215.tmp) -> $(tail -1 $S/mon-soak-js215.tmp)"
docker run --rm -v mqb-js:/data alpine du -sh /data
echo "== backlog js 3M"
start_js; m=$(mon $c $S/mon-bl-js215.tmp)
$B -sys js -mode pub -n 3000000 -c 4 -async 1000 | grep RESULT
$B -sys js -mode sub -n 3000004 -c 16 -setup=false | grep RESULT
kill $m; summ $S/mon-bl-js215.tmp
docker inspect -f 'OOMKilled={{.State.OOMKilled}} Status={{.State.Status}}' $c
docker logs $c 2>&1 | grep -cE '\[ERR\]|FATAL|panic' | sed 's/^/server error lines: /'
docker rm -f mqb-nsq mqb-js >/dev/null; docker volume rm -f mqb-nsq mqb-js >/dev/null
echo ALL_DONE
