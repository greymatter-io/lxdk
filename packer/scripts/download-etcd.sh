#!/bin/bash
set -u
tmp_dir="$(mktemp -d /tmp/kubedee-XXXXXX)"
(
    cd "${tmp_dir}"
    echo "Fetching etcd ${etcd_version} ..."
    curl -fsSL -o - "https://github.com/etcd-io/etcd/releases/download/${etcd_version}/etcd-${etcd_version}-linux-amd64.tar.gz" |
      tar -xzf - --strip-components 1
    mv etcd /usr/local/bin/
    mv etcdctl /usr/local/bin/
)
rm -rf "${tmp_dir}"
