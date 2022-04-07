#!/bin/bash
set -eu

tmp_dir="$(mktemp -d /tmp/kubedee-XXXXXX)"
(
    cd "${tmp_dir}"
    echo "Fetching docker registry..."
    curl -fsSL -o - "https://github.com/distribution/distribution/releases/download/v2.8.1/registry_2.8.1_linux_amd64.tar.gz" |
      tar -xzf -
    cp registry /usr/local/bin
)
rm -rf "${tmp_dir}"
