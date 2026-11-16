source "$(dirname "$0")/lib.sh"
run() { # sys startfn args...
  local sys=$1 st=$2; shift 2
  $st; local name=mqb-$sys; [ $sys = js ] || name=mqb-nsq
  local m=$(mon $name $S/mon.tmp); $B -sys $sys "$@" | grep RESULT; kill $m; wait $m 2>/dev/null; summ $S/mon.tmp
}
for sys in nsq js; do
  st=start_$sys
  echo "== $sys sync pub 1KB"
  for c in 1 16 64; do n=$((c==1?50000:300000)); run $sys $st -mode pub -n $n -c $c; done
  echo "== $sys sync pub 128B c=64"; run $sys $st -mode pub -n 300000 -c 64 -size 128
  echo "== $sys async pub 1KB c=4 window=1000 then drain"
  $st; name=mqb-nsq; [ $sys = js ] && name=mqb-js
  m=$(mon $name $S/mon.tmp); $B -sys $sys -mode pub -n 1000000 -c 4 -async 1000 | grep RESULT; kill $m; summ $S/mon.tmp
  m=$(mon $name $S/mon.tmp); $B -sys $sys -mode sub -n 1000004 -c 16 -setup=false | grep RESULT; kill $m; summ $S/mon.tmp
done
echo "== nsq (默认 mem-queue-size=10000) 参考"
start_nsq_mem() { start_nsq "--mem-queue-size=10000"; }
run nsq start_nsq_mem -mode pub -n 300000 -c 64
docker rm -f mqb-nsq mqb-js >/dev/null
