#!/bin/bash

# TODO: use variable for version

set -eu

apt-get install wget runc

wget -q "https://storage.googleapis.com/cri-o/artifacts/cri-o.amd64.c0b2474b80fd0844b883729bda88961bed7b472b.tar.gz"
tar -xvf "cri-o.amd64.c0b2474b80fd0844b883729bda88961bed7b472b.tar.gz"

bin_dir="cri-o/bin"
cp ${bin_dir}/crio /usr/local/bin/
cp ${bin_dir}/conmon /usr/local/bin/
cp ${bin_dir}/pinns /usr/local/bin/

mkdir -p /etc/crio
cp cri-o/etc/crio.conf /etc/crio/
cp cri-o/etc/crictl.yaml /etc/crio/
cp cri-o/etc/crio-umount.conf /etc/crio/
cp cri-o/contrib/policy.json /etc/crio/
