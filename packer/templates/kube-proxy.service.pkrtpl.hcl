[Unit]
Description=Kubernetes Kube Proxy

[Service]
ExecStart=/usr/local/bin/kube-proxy \
  --cluster-cidr=10.200.0.0/16 \
  --kubeconfig=/etc/kubernetes/kube-proxy.kubeconfig \
  --proxy-mode=iptables \
  --conntrack-max-per-core=0 \
  --v=2
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target

