# 服务端：2 核 (cpuset 0,1) 1G 内存、无 swap；客户端用 taskset 绑 2-5 核
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
# 中间输出（采样、日志）放在 out/，不提交
S=${OUT:-$ROOT/out}; mkdir -p "$S"
LIM="--cpus=2 --cpuset-cpus=0,1 --memory=1g --memory-swap=1g"
B="taskset -c 2-5 $ROOT/tools/mqbench/mqbench"
start_nsq() { # $1 = extra nsqd args
  docker rm -f mqb-nsq >/dev/null 2>&1; docker volume rm -f mqb-nsq >/dev/null 2>&1
  docker run -d --name mqb-nsq $LIM -v mqb-nsq:/data -p 127.0.0.1:14150:4150 -p 127.0.0.1:14151:4151 \
    nsqio/nsq:v1.3.0 /nsqd --data-path=/data ${1:---mem-queue-size=0} >/dev/null
  until curl -sf 127.0.0.1:14151/ping >/dev/null; do sleep 0.2; done
}
start_js() {
  docker rm -f mqb-js >/dev/null 2>&1; docker volume rm -f mqb-js >/dev/null 2>&1
  docker run -d --name mqb-js $LIM -v mqb-js:/data -p 127.0.0.1:14222:4222 -p 127.0.0.1:18222:8222 \
    ${NATS_IMAGE:-nats:2.12-alpine} nats-server -js -sd /data -m 8222 >/dev/null
  until curl -sf 127.0.0.1:18222/healthz >/dev/null; do sleep 0.2; done
}
cg() { echo /sys/fs/cgroup/system.slice/docker-$(docker inspect -f '{{.Id}}' $1).scope; }
# 后台采样：每秒 CPU%、anon(进程内存)、current(含页缓存)、oom 次数
mon() { # $1 container $2 outfile
  ( d=$(cg $1); pu=$(awk '/usage_usec/{print $2}' $d/cpu.stat); pt=$(date +%s%N)
    while [ -d $d ]; do sleep 1
      u=$(awk '/usage_usec/{print $2}' $d/cpu.stat 2>/dev/null) || break; t=$(date +%s%N)
      a=$(awk '/^anon /{print $2}' $d/memory.stat); c=$(cat $d/memory.current); o=$(awk '/oom_kill /{print $2}' $d/memory.events)
      echo "$(( (u-pu)*100000/((t-pt)/1000) /1000 )) $((a>>20)) $((c>>20)) $o"; pu=$u; pt=$t
    done ) > $2 2>/dev/null &
  echo $!
}
# 汇总采样: 平均/峰值 CPU%，峰值 anon MB，峰值 current MB，oom
summ() { awk 'NF==4{n++; s+=$1; if($1>mc)mc=$1; if($2>ma)ma=$2; if($3>mx)mx=$3; o=$4} END{printf "STATS cpu_avg=%.0f%% cpu_max=%d%% anon_max=%dMB cur_max=%dMB oom=%d samples=%d\n", s/n, mc, ma, mx, o, n}' $1; }
