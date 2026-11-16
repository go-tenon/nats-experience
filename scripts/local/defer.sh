source "$(dirname "$0")/lib.sh"
# $1 sys  $2 n  $3 inflight  $4 kill|none
sys=$1; n=$2; inf=$3; c=mqb-$sys
start_$sys; m=$(mon $c $S/mon-defer.tmp)
echo "== defer sys=$sys n=$n inflight=$inf action=$4"
$B -sys $sys -mode defer -n $n -inflight $inf -c 32 -delay 60s -wait 90s ${MAXACK:+-maxack $MAXACK} > $S/defer.out 2>&1 & p=$!
for i in $(seq 1 90); do grep -q DEFERRED_ALL $S/defer.out && break; sleep 1; done
grep -E 'DEFERRED_ALL|published' $S/defer.out; sleep 3
echo "mem with all deferred: $(tail -1 $S/mon-defer.tmp)  (cpu% anonMB curMB oom)"; tail -1 $S/defer.out
if [ "$4" = kill ]; then docker kill -s KILL $c >/dev/null; sleep 2; docker start $c >/dev/null; fi
wait $p; kill $m 2>/dev/null; grep RESULT $S/defer.out; summ $S/mon-defer.tmp
docker inspect -f 'OOMKilled={{.State.OOMKilled}} Status={{.State.Status}}' $c
