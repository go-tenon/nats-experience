source "$(dirname "$0")/lib.sh"
for sys in nsq js; do for r in 1000 5000 10000 20000; do
  start_$sys; name=mqb-$sys
  m=$(mon $name $S/mon.tmp); echo "== $sys rate=$r"
  $B -sys $sys -mode e2e -rate $r -dur 45s -c 64 | grep -E 'RESULT|WIN'; kill $m; summ $S/mon.tmp
done; done
docker rm -f mqb-nsq mqb-js >/dev/null
