#!/usr/bin/env bash
# 部署或更新多产品共用的 NATS。可重复运行，已有密码沿用。
#
#   bash install-nats.sh product_a product_b    # 参数为产品用户名，账号名为其大写；须列出全部产品
#
# 环境变量：
#   ALLOW_CIDR   允许访问 4222 的网段，默认 172.16.0.0/12
#   PRIVATE_IP   监听地址，默认 eth0 的地址
#   QUOTA_FILE   每个账号的磁盘配额，默认 5GB
#   QUOTA_MEM    每个账号的内存配额，默认 64MB
#   NATS_IMAGE   默认 nats:2.15.3-alpine
#   SERVER_NAME  默认沿用现有配置，否则为主机名
#
# 产出：/etc/nats/nats.conf；/etc/nats/<用户>.password（仅 root 可读）；容器 nats；ufw 规则。
set -euo pipefail
[ $# -ge 1 ] || { echo "用法：$0 <产品用户名>..." >&2; exit 2; }
IMAGE=${NATS_IMAGE:-nats:2.15.3-alpine}
IP=${PRIVATE_IP:-$(ip -4 -o addr show eth0 | awk '{print $4}' | cut -d/ -f1)}
CIDR=${ALLOW_CIDR:-172.16.0.0/12}
QF=${QUOTA_FILE:-5GB}; QM=${QUOTA_MEM:-64MB}
NAME=${SERVER_NAME:-$(sudo sed -n 's/^server_name: *//p' /etc/nats/nats.conf 2>/dev/null | head -1)}
NAME=${NAME:-$(hostname -s)}

sudo mkdir -p /etc/nats
docker pull -q "$IMAGE" >/dev/null
docker pull -q natsio/nats-box:latest >/dev/null

# 生成或沿用用户的密码，输出 bcrypt 哈希
hash() {
  local f=/etc/nats/$1.password
  if ! sudo test -s "$f"; then
    openssl rand -base64 60 | tr -dc 'A-Za-z0-9' | head -c 40 | sudo tee "$f" >/dev/null
    sudo chmod 600 "$f"
  fi
  PASSWORD="$(sudo cat "$f")" docker run --rm -e PASSWORD natsio/nats-box:latest nats server passwd 2>/dev/null \
    | grep -oE '\$2[aby]\$[0-9]+\$[^ ]+'
}

accounts=""
for user in "$@"; do
  h=$(hash "$user"); [ ${#h} -eq 60 ] || { echo "生成 $user 的哈希失败" >&2; exit 1; }
  acct=$(echo "$user" | tr '[:lower:]' '[:upper:]')
  accounts+="  $acct: {
    jetstream: { max_file: $QF, max_mem: $QM, max_streams: 20, max_consumers: 200 }
    users: [ { user: $user, password: \"$h\" } ]
  }
"
done
hs=$(hash sys)

[ -f /etc/nats/nats.conf ] && sudo cp /etc/nats/nats.conf "/etc/nats/nats.conf.$(date +%Y%m%d%H%M%S).bak"
sudo tee /etc/nats/nats.conf >/dev/null <<CONF
# 由 install-nats.sh 生成
server_name: $NAME

# 只监听内网；公网访问由安全组和 ufw 限制
listen: $IP:4222
# 监控，仅本机
http: 127.0.0.1:8222

jetstream {
  store_dir: /data
  # 每秒同步磁盘，断电最多丢约 1 秒的写入
  sync_interval: "1s"
  max_file_store: 30GB
  max_memory_store: 256MB
}

# 每个产品一个账号。修改后 docker kill -s HUP nats
accounts {
$accounts  SYS: {
    users: [ { user: sys, password: "$hs" } ]
  }
}
system_account: SYS
CONF
sudo chmod 640 /etc/nats/nats.conf

if docker ps -a --format '{{.Names}}' | grep -qx nats; then
  since=$(date -u +%Y-%m-%dT%H:%M:%SZ); docker kill -s HUP nats >/dev/null; sleep 2
  if docker logs --since "$since" nats 2>&1 | grep -q 'Reloaded server configuration'; then
    echo "已重新加载配置"
  else
    echo "重新加载失败，见 docker logs nats。监听地址、jetstream 段的改动需要 docker restart nats" >&2
    docker logs --since "$since" nats 2>&1 | tail -3 >&2; exit 1
  fi
else
  docker run -d --name nats --restart always --network host \
    -e GOMEMLIMIT=1200MiB --log-opt max-size=50m --log-opt max-file=3 \
    -v /etc/nats/nats.conf:/etc/nats/nats.conf:ro -v nats-data:/data \
    "$IMAGE" nats-server -c /etc/nats/nats.conf >/dev/null
fi
for i in $(seq 1 30); do curl -sf '127.0.0.1:8222/healthz?js-enabled-only=true' >/dev/null && break; sleep 1; done
curl -s '127.0.0.1:8222/healthz?js-enabled-only=true'; echo

sudo ufw allow OpenSSH >/dev/null
sudo ufw allow from "$CIDR" to any port 4222 proto tcp >/dev/null
sudo ufw default deny incoming >/dev/null
sudo ufw default allow outgoing >/dev/null
sudo ufw --force enable >/dev/null
sudo ufw status | sed -n 1,10p
echo "应用的连接地址：nats://<用户>:<密码>@$IP:4222，密码：sudo cat /etc/nats/<用户>.password"
