#!/bin/bash 

set -e

sudo apt-get update
sudo apt-get install -y \
    kitty-terminfo \
    gcc \
    snapd \
    unzip

if ! go version &>/dev/null; then
    wget "https://go.dev/dl/go1.18.linux-amd64.tar.gz"
    rm -rf /usr/local/go && sudo tar -C /usr/local -xzf "go1.18.linux-amd64.tar.gz"
    echo "export PATH=$PATH:/usr/local/go/bin:/home/vagrant/go/bin" >> ~/.profile
fi

export PATH="$PATH:/usr/local/go/bin:/home/vagrant/go/bin"

if ! lxd &>/dev/null; then
    sudo snap install lxd --channel=4.0/stable
fi

sudo adduser vagrant lxd
echo "vagrant" | su vagrant

wget "https://releases.hashicorp.com/packer/1.8.0/packer_1.8.0_linux_amd64.zip"
unzip -o "packer_1.8.0_linux_amd64.zip"
sudo cp packer /usr/bin/packer

cat <<EOF | sudo lxd init --preseed
config: {}
networks:
- config:
    ipv4.address: auto
    ipv6.address: auto
  description: ""
  name: lxdbr0
  type: ""
storage_pools:
- config:
    size: 7GB
  description: ""
  name: default
  driver: btrfs
profiles:
- config: {}
  description: ""
  devices:
    eth0:
      name: eth0
      network: lxdbr0
      type: nic
    root:
      path: /
      pool: default
      type: disk
  name: default
cluster: null
EOF

go install github.com/cloudflare/cfssl/cmd/cfssl@latest
go install github.com/cloudflare/cfssl/cmd/cfssljson@latest

curl -LO https://dl.k8s.io/release/v1.23.0/bin/linux/amd64/kubectl
chmod +x kubectl
sudo cp kubectl /usr/local/bin

alias lxdk="go run ./cmd/lxdk"
echo "export KUBECONFIG=~/.cache/lxdk/gm/kubeconfigs/admin.kubeconfig" >> ~/.profile
