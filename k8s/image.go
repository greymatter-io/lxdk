package k8s

// k8s version
var (
	// need "v"?
	k8sVersion = "1.23.1"

	// v?
	crioVersion = "1.21.4"

	// v?
	etcdVersion = "3.4.14"
)

func crioURL(version string) string {
	switch version {
	case "1.21.4":
		return "https://storage.googleapis.com/k8s-conform-cri-o/artifacts/cri-o.amd64.61748dc51bdf1af367b8a68938dbbc81c593b95d.tar.gz"
	}
	return ""
}

// etcd image

// controller/api server

// worker node

var commonPackages = []string{"libgpgme11", "kitty-terminfo", "htop", "jq", "socat", "curl", "iptables", "cloud-init"}

func installCNIPlugins(version string) error {
	/*
	   "mkdir -p /opt/cni/bin",
	   "curl -fsSL https://github.com/containernetworking/plugins/releases/download/v1.1.1/cni-plugins-linux-amd64-v1.1.1.tgz | tar -xzC /opt/cni/bin",
	   "curl -fsSLo /opt/cni/bin/flannel https://github.com/flannel-io/cni-plugin/releases/download/v1.0.1/flannel-amd64",
	   "chmod +x /opt/cni/bin/flannel",
	*/

	return nil
}

func downloadK8s(version string) error {

	/*
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
	*/

	return nil
}

func downloadETCD(version string) error {
	/*
		curl -fsSL -o - "https://github.com/etcd-io/etcd/releases/download/${etcd_version}/etcd-${etcd_version}-linux-amd64.tar.gz" |
	    tar -xzf - --strip-components 1
	    mv etcd /usr/local/bin/
	    mv etcdctl /usr/local/bin/
	*/
	return nil
}

func downloadCRIO(version string) error {
	/*

	apt-get install wget runc

	wget "https://storage.googleapis.com/cri-o/artifacts/cri-o.amd64.c0b2474b80fd0844b883729bda88961bed7b472b.tar.gz"
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

	*/
	return nil
}

func downloadRegistry(version string) error {
	/*
		    echo "Fetching docker registry..."
	    curl -fsSL -o - "https://github.com/distribution/distribution/releases/download/v2.8.1/registry_2.8.1_linux_amd64.tar.gz" |
	      tar -xzf -
	    cp registry /usr/local/bin

	*/
	return nil
}

func templateETCDUnitFile() error {
	// EnvironmentFile=/etc/etcd/env  (should be) /etc/defaults/etcd
	return nil
}

func templateKubeProxyUnitFile() error {
	// controller and worker
	return nil
}

func templateKubeletUnitfile() error {
	// controller and worker
	return nil
}

func templateCRIOUnitFile() error {
	// controller and worker
	return nil
}

func templateKubeSchedulerUnitFile() error {
	// controller
	return nil
}

func templateKubeAPIServerUnitFile() error {
	// controller
	return nil
}

func templateKubeControllerManagerUnitFile() error {
	// controller
	return nil
}

func templateOCIRegistryUnitFile() error {
	// controller and worker
	return nil
}

func templateRegistryConfigFile() error {
	// controller and worker
	return nil
}

func templateKubeSchedulerconfigFile() error {
	// controller and worker
	return nil
}

func startEnableCRIO() error {
	// controller and worker
	return nil
}
