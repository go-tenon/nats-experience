#!/usr/bin/env bash
# 在一台 Ubuntu 上部署多产品共用的单机 NATS（JetStream + 每个产品一个账号）。可以重复运行：已有的密码不会改变。
#
#   bash install-nats.sh product_a product_b      # 每个参数是一个产品的用户名，账号名是它的大写
#
# 环境变量：
#   NATS_IMAGE   默认 nats:2.15.3-alpine（固定版本）
#   PRIVATE_IP   默认取 eth0 的地址，NATS 只监听它
#   ALLOW_CIDR   允许访问 4222 的网段，默认 172.16.0.0/12（阿里云 VPC 常用网段，按实际 VPC 修改）
#   QUOTA_FILE   每个产品的 JetStream 磁盘配额，默认 5GB
#   QUOTA_MEM    每个产品的 JetStream 内存配额，默认 64MB
#   SERVER_NAME  默认沿用已有配置中的 server_name，没有时用主机名（重新加载时不能修改它）
#
# 结果：/etc/nats/nats.conf、每个用户的明文密码 /etc/nats/<用户>.password（只有 root 可读），
# 容器 nats（host 网络、自动重启），ufw 只放行 SSH 和 ALLOW_CIDR 访问 4222。
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

# hash 用户：生成（或沿用）这个用户的密码，输出 bcrypt 哈希。明文只保存在 /etc/nats/<用户>.password。
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
# NATS 多产品共用配置。由 install-nats.sh 生成。
server_name: $NAME

# 只监听内网。云厂商的公网 IP 通常经 NAT 映射到这个地址，所以另由防火墙限制来源。
listen: $IP:4222
# 监控端口只给本机
http: 127.0.0.1:8222

jetstream {
  store_dir: /data
  # 每秒同步一次磁盘：断电时最多丢最近约 1 秒的写入。always 在普通云盘上只有约 450 条/秒。
  sync_interval: "1s"
  max_file_store: 30GB
  max_memory_store: 256MB
}

# 每个产品一个账号：subject 和 stream 互相隔离，JetStream 资源单独限额。
# 增删账号、改配额后：docker kill -s HUP nats
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
