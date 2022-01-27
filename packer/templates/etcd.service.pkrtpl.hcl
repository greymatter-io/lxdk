[Unit]
Description=etcd

[Service]
EnvironmentFile=/etc/etcd/env
ExecStart=/usr/local/bin/etcd \
  --name "${CONTAINER_NAME}" \
  --cert-file="/etc/etcd/etcd.pem" \
  --key-file="/etc/etcd/etcd-key.pem" \
  --peer-cert-file="/etc/etcd/etcd.pem" \
  --peer-key-file="/etc/etcd/etcd-key.pem" \
  --trusted-ca-file="/etc/etcd/ca-etcd.pem" \
  --peer-trusted-ca-file="/etc/etcd/ca-etcd.pem" \
  --peer-client-cert-auth \
  --client-cert-auth \
  --initial-advertise-peer-urls "https://${ETCD_IP}:2380" \
  --listen-peer-urls "https://${ETCD_IP}:2380" \
  --listen-client-urls "https://${ETCD_IP}:2379,http://127.0.0.1:2379" \
  --advertise-client-urls "https://${ETCD_IP}:2379" \
  --initial-cluster-token "etcd-cluster-0" \
  --initial-cluster "${CONTAINER_NAME}=https://${ETCD_IP}:2380" \
  --initial-cluster-state "new" \
  --data-dir="/var/lib/etcd"
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
