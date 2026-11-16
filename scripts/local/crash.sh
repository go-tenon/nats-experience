source "$(dirname "$0")/lib.sh"
for sys in nsq js; do for kind in kill restart; do
  start_$sys; c=mqb-$sys
  echo "== crash sys=$sys kind=$kind"
  $B -sys $sys -mode crash -n 200000 -rate 5000 -c 16 -inflight 200 -work 2ms -wait 60s > $S/crash.out 2>&1 &
  p=$!; sleep 15
  if [ $kind = kill ]; then docker kill -s KILL $c >/dev/null; sleep 3; docker start $c >/dev/null
  else t0=$(date +%s%N); docker restart -t 10 $c >/dev/null; echo "restart took $(( ($(date +%s%N)-t0)/1000000 ))ms"; fi
  wait $p; grep -E 'RESULT' $S/crash.out; grep -vE 'RESULT|PROG' $S/crash.out | sort | uniq -c | sort -rn | head -3
done; done
docker rm -f mqb-nsq mqb-js >/dev/null
