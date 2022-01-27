#!/bin/bash

set -u

dl_dir="/tmp/download-k8s-${RANDOM}"
mkdir -p $dl_dir
pushd $dl_dir
echo "downloading Kubernetes ${k8s_version}"
if ! curl -fsSLI "https://dl.k8s.io/${k8s_version}/kubernetes-server-linux-amd64.tar.gz" >/dev/null; then
    echo "Kubernetes version '${k8s_version}' not found on dl.k8s.io"
    exit 1
fi
curl -fsSL -o - "https://dl.k8s.io/${k8s_version}/kubernetes-server-linux-amd64.tar.gz" | \
  tar -xzf - --strip-components 3 \
     "kubernetes/server/bin/"{kube-apiserver,kube-controller-manager,kubectl,kubelet,kube-proxy,kube-scheduler}

mv * /usr/local/bin/