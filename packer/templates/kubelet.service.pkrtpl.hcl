[Unit]
Description=Kubernetes Kubelet
After=crio.service
Requires=crio.service

[Service]
EnvironmentFile=/etc/lxdk/env
ExecStart=/usr/local/bin/kubelet \
  --cgroup-driver=systemd \
  --config=/etc/kubernetes/config/kubelet.yaml \
  --container-runtime=remote \
  --container-runtime-endpoint=unix:///var/run/crio/crio.sock \
  --image-service-endpoint=unix:///var/run/crio/crio.sock \
  --kubeconfig=/etc/kubernetes/${CONTAINER_NAME}-kubelet.kubeconfig \
  --network-plugin=cni \
  --register-node=true \
  --v=2
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
