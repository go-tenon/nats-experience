# Ubuntu 上安装 Docker 和 sysstat（iostat）。
set -e
sudo DEBIAN_FRONTEND=noninteractive apt-get update -qq
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq docker.io sysstat
sudo usermod -aG docker "$USER"
sudo systemctl enable --now docker
