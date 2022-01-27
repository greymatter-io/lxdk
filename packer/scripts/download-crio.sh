#!/bin/bash

set -u

dl_dir="$(mktemp -d /tmp/kubedee-crio-XXXXXX)"
pushd $dl_dir

curl -fsSl -o - "$crio_url" | tar -xzf -
mv cri-o/bin/* /usr/local/bin/