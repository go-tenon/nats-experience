source "$(dirname "$0")/lib.sh"
for sys in nsq js; do
  c=mqb-$sys; start_$sys; m=$(mon $c $S/mon-soak-$sys.tmp)
  echo "== soak sys=$sys 10k/s 10min"
  $B -sys $sys -mode e2e -rate 10000 -dur 600s -c 64 | grep -E 'RESULT|WIN'
  kill $m; summ $S/mon-soak-$sys.tmp
  echo "mem first/last min (cpu anon cur oom): $(sed -n '30p' $S/mon-soak-$sys.tmp) -> $(tail -1 $S/mon-soak-$sys.tmp)"
  docker run --rm -v mqb-$sys:/data alpine du -sh /data
  echo "== backlog sys=$sys 3M x 1KB 无消费者写入后排空"
  start_$sys; m=$(mon $c $S/mon-bl-$sys.tmp)
  $B -sys $sys -mode pub -n 3000000 -c 4 -async 1000 | grep RESULT
  docker run --rm -v mqb-$sys:/data alpine du -sh /data
  $B -sys $sys -mode sub -n 3000004 -c 16 -setup=false | grep RESULT
  kill $m; summ $S/mon-bl-$sys.tmp
  docker inspect -f 'OOMKilled={{.State.OOMKilled}} Status={{.State.Status}}' $c
  docker logs $c 2>&1 | grep -cE 'ERROR|FATAL|panic' | sed 's/^/server error lines: /'
done
docker rm -f mqb-nsq mqb-js >/dev/null
