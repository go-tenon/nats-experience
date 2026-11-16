# 安装 Docker（Ubuntu）
set -e
sudo DEBIAN_FRONTEND=noninteractive apt-get update -qq
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq docker.io
sudo usermod -aG docker "$USER"
sudo systemctl enable --now docker
