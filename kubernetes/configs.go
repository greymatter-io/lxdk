package kubernetes

import "fmt"

// TODO: better way to do this?
func WorkerRegistriesConfig(registryName, registryIP string) []byte {
	return []byte(fmt.Sprintf(`unqualified-search-registries = ['docker.io']
[[registry]]
prefix = "registry.local:5000"
insecure = true
location = "%s:5000"
[[registry]]
prefix = "%s:5000"
insecure = true
location = "%s:5000"
[[registry]]
prefix = "%s:-127.0.0.1}:5000"
insecure = true
location = "%s:-127.0.0.1}:5000"`, registryName, registryName, registryName, registryIP, registryIP))
}

// this one could be removed by not putting the unique container ID for the
// kubelet in the name of the cert in the container itslef
func KubeletConfig(containerName string) []byte {
	return []byte(fmt.Sprintf(`kind: KubeletConfiguration
apiVersion: kubelet.config.k8s.io/v1beta1
authentication:
  anonymous:
    enabled: false
  webhook:
    enabled: true
  x509:
    clientCAFile: "/etc/kubernetes/ca.pem"
authorization:
  mode: Webhook
cgroupDriver: systemd
clusterDomain: "cluster.local"
clusterDNS:
  - "10.32.0.10"
podCIDR: "10.20.0.0/16"
runtimeRequestTimeout: "10m"
tlsCertFile: "/etc/kubernetes/%s.pem"
tlsPrivateKeyFile: "/etc/kubernetes/%s-key.pem"
failSwapOn: false
evictionHard: {}
enforceNodeAllocatable: []
maxPods: 1000
# TODO(schu): check if issues were updated
# https://github.com/kubernetes/kubernetes/issues/66067
# https://github.com/kubernetes-sigs/cri-o/issues/1769
#resolverConfig: /run/systemd/resolve/resolv.conf
#resolverConfig: /var/run/netconfig/resolv.conf`, containerName, containerName))
}

func KubeletUnitConfig(containerName string) []byte {
	return []byte(fmt.Sprintf(`[Unit]
Description=Kubernetes Kubelet
After=crio.service
Requires=crio.service
[Service]
ExecStart=/usr/local/bin/kubelet \
  --config=/etc/kubernetes/config/kubelet.yaml \
  --container-runtime=remote \
  --container-runtime-endpoint=unix:///var/run/crio/crio.sock \
  --image-service-endpoint=unix:///var/run/crio/crio.sock \
  --kubeconfig=/etc/kubernetes/%s-kubelet.kubeconfig \
  --register-node=true \
  --v=2
Restart=on-failure
RestartSec=5
[Install]
WantedBy=multi-user.target`, containerName))
}
