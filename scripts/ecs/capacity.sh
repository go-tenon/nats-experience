# 单机容量：PostgreSQL、NATS、应用（tools/tenonload）在同一台机器上，按速率逐级加压。
# 容器必须用 host 网络：用 -p 端口映射时，docker-proxy 在 2 核机器上要占约 40% 的 CPU，上限从约 2350/s 掉到约 1300/s。
# 先在本地编译：cd tools/tenonload && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o tenonload，再上传到 ~/tenonload。
set -e
docker rm -f pg nats >/dev/null 2>&1 || true
docker volume rm -f pgdata natsdata >/dev/null 2>&1 || true
docker run -d --name pg --network host -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=bench -v pgdata:/var/lib/postgresql postgres:18 \
  -c listen_addresses=127.0.0.1 -c wal_level=logical -c max_replication_slots=10 -c max_wal_senders=10 -c shared_buffers=256MB >/dev/null
docker run -d --name nats --network host -v natsdata:/data nats:2.15-alpine nats-server -js -sd /data -a 127.0.0.1 -m 8222 >/dev/null
until docker exec pg pg_isready -h 127.0.0.1 -U postgres -q && curl -sf 127.0.0.1:8222/healthz >/dev/null; do sleep 1; done; sleep 2
./tenonload -mode setup
CG="pg=/sys/fs/cgroup/system.slice/docker-$(docker inspect -f '{{.Id}}' pg).scope,nats=/sys/fs/cgroup/system.slice/docker-$(docker inspect -f '{{.Id}}' nats).scope"
echo "######## 处理函数为空"
./tenonload -mode run -rates 500,1000,1500,2000,0 -step 45s -cgroups "$CG" 2>&1 | grep -E '^(==|RESULT|DRAIN)'
echo "######## 处理函数更新一行数据"
./tenonload -mode run -work db -rates 500,1000,1500,0 -step 45s -cgroups "$CG" 2>&1 | grep -E '^(==|RESULT|DRAIN)'
echo "######## 定时消息的内存：先 10 万条，再追加 20 万条，最后重启 NATS 看干净的占用"
./tenonload -mode defer -n 100000 -cgroups "$CG" 2>&1 | grep -v '^time=' | tail -1
./tenonload -mode defer -n 200000 -cgroups "$CG" 2>&1 | grep -v '^time=' | tail -1
docker restart nats >/dev/null; until curl -sf 127.0.0.1:8222/healthz >/dev/null; do sleep 1; done; sleep 5
CG="pg=/sys/fs/cgroup/system.slice/docker-$(docker inspect -f '{{.Id}}' pg).scope,nats=/sys/fs/cgroup/system.slice/docker-$(docker inspect -f '{{.Id}}' nats).scope"
echo "after restart: $(./tenonload -mode defer -n 0 -cgroups "$CG" 2>/dev/null | tail -1)"
