[Unit]
Description=Kubernetes API Server

[Service]
EnvironmentFile=/etc/lxdk/env
ExecStart=/usr/local/bin/kube-apiserver \
  --allow-privileged=true \
  --apiserver-count=3 \
  --audit-log-maxage=30 \
  --audit-log-maxbackup=3 \
  --audit-log-maxsize=100 \
  --audit-log-path=/var/log/audit.log \
  --authorization-mode=Node,RBAC \
  --bind-address=0.0.0.0 \
  --client-ca-file=/etc/kubernetes/ca.pem \
  --enable-admission-plugins="${ADMISSION_PLUGINS}" \
  --enable-swagger-ui=true \
  --etcd-cafile=/etc/kubernetes/ca-etcd.pem \
  --etcd-certfile=/etc/kubernetes/etcd.pem \
  --etcd-keyfile=/etc/kubernetes/etcd-key.pem \
  --etcd-servers="https://${ETCD_IP}:2379" \
  --event-ttl=1h \
  --kubelet-certificate-authority=/etc/kubernetes/ca.pem \
  --kubelet-client-certificate=/etc/kubernetes/kubernetes.pem \
  --kubelet-client-key=/etc/kubernetes/kubernetes-key.pem \
  --runtime-config=rbac.authorization.k8s.io/v1alpha1 \
  --service-account-issuer="https://api" \
  --service-account-signing-key-file=/etc/kubernetes/ca-key.pem \
  --service-account-api-audiences=kubernetes.default.svc \
  --service-account-key-file=/etc/kubernetes/ca-key.pem \
  --service-cluster-ip-range=10.32.0.0/24 \
  --service-node-port-range=30000-32767 \
  --tls-cert-file=/etc/kubernetes/kubernetes.pem \
  --tls-private-key-file=/etc/kubernetes/kubernetes-key.pem \
  --proxy-client-cert-file=/etc/kubernetes/aggregation-client.pem \
  --proxy-client-key-file=/etc/kubernetes/aggregation-client-key.pem \
  --requestheader-client-ca-file=/etc/kubernetes/ca-aggregation.pem \
  --requestheader-extra-headers-prefix=X-Remote-Extra- \
  --requestheader-group-headers=X-Remote-Group \
  --requestheader-username-headers=X-Remote-User \
  --v=2
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target

