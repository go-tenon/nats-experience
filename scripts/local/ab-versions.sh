# 两个 NATS 版本交替运行多轮吞吐测试，取中位数比较。
# 在有其他负载的机器上，单次测试的结果可能相差近一倍，不能只跑一轮。
# 用法：bash ab-versions.sh 2.12 2.15 [轮数]
source "$(dirname "$0")/lib.sh"
A=${1:-2.12}; Bv=${2:-2.15}; ROUNDS=${3:-4}; out=$S/ab.txt; : > "$out"
start() { NATS_IMAGE=nats:$1-alpine start_js; }
rate() { grep -o 'rate=[0-9]*' | cut -d= -f2; }
for round in $(seq 1 $ROUNDS); do for v in $A $Bv; do
  start $v; c16=$($B -sys js -mode pub -n 300000 -c 16 | rate)
  start $v; c64=$($B -sys js -mode pub -n 300000 -c 64 | rate)
  start $v; asy=$($B -sys js -mode pub -n 1000000 -c 4 -async 1000 | rate); drn=$($B -sys js -mode sub -n 1000004 -c 16 -setup=false | rate)
  echo "$round $v $c16 $c64 $asy $drn $(cut -d' ' -f1 /proc/loadavg)" | tee -a "$out"
done; done
docker rm -f mqb-js >/dev/null; docker volume rm -f mqb-js >/dev/null
python3 - "$out" <<'PY'
import sys, statistics as st
rows=[l.split() for l in open(sys.argv[1])]
print("\n中位数（msg/s）  sync c=16  sync c=64  async 写入  排空   （最后一列是当时的 load average）")
for v in sorted({r[1] for r in rows}):
    r=[x for x in rows if x[1]==v]
    print(v, *[int(st.median(int(x[i]) for x in r)) for i in range(2,6)])
    print("  范围", *[f"{min(int(x[i]) for x in r)//1000}-{max(int(x[i]) for x in r)//1000}k" for i in range(2,6)])
PY
