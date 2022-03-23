package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/greymatter-io/lxdk/certificates"
	certs "github.com/greymatter-io/lxdk/certificates"
	"github.com/greymatter-io/lxdk/config"
	"github.com/greymatter-io/lxdk/containers"
	"github.com/greymatter-io/lxdk/kubernetes"
	"github.com/greymatter-io/lxdk/lxd"
	lxdclient "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/urfave/cli/v2"
)

var (
	startCmd = &cli.Command{
		Name:   "start",
		Usage:  "start a cluster",
		Action: doStart,
	}
)

// TODO: apiserver flags should be configurable, use a .env file for
// the apiserver systemd service
// TODO: split this up into individual commands, each with their own tests to
// make sure each service is configured correctly
func doStart(ctx *cli.Context) error {
	cacheDir := ctx.String("cache")
	if ctx.Args().Len() == 0 {
		return fmt.Errorf("must supply cluster name")
	}
	clusterName := ctx.Args().First()
	certDir := path.Join(cacheDir, clusterName, "certificates")

	state, err := config.ClusterStateFromContext(ctx)
	if err != nil {
		return err
	}

	is, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

	for _, container := range state.Containers {
		log.Default().Println("starting " + container)
		err = containers.StartContainer(container, is)
		if err != nil {
			return err
		}

	}

	// etcd cert
	etcdIP, err := containers.GetContainerIP(state.EtcdContainerName, is)
	if err != nil {
		return err
	}
	etcdCertConfig := certs.CertConfig{
		Name: "etcd",
		CN:   "etcd",
		CA: certs.CAConfig{
			Name: "ca-etcd",
			Dir:  certDir,
			CN:   "etcd",
		},
		Dir:          certDir,
		CAConfigPath: path.Join(certDir, "ca-config.json"),
		ExtraOpts: map[string]string{
			"hostname": etcdIP + ",127.0.0.1",
		},
	}

	err = certificates.CreateCert(etcdCertConfig)
	if err != nil {
		return err
	}

	// controller cert
	controllerIP, err := containers.GetContainerIP(state.ControllerContainerName, is)
	if err != nil {
		return err
	}
	controllerCertConfig := certs.CertConfig{
		Name: "kubernetes",
		CN:   "kubernetes",
		CA: certs.CAConfig{
			Name: "ca",
			Dir:  certDir,
			CN:   "Kubernetes",
		},
		Dir:          certDir,
		CAConfigPath: path.Join(certDir, "ca-config.json"),
		ExtraOpts: map[string]string{
			"hostname": controllerIP + ",127.0.0.1",
		},
	}

	err = certificates.CreateCert(controllerCertConfig)
	if err != nil {
		return err
	}

	// TODO: probably going to have to move this out
	// worker cert
	workerContainers := state.WorkerContainerNames
	workerContainers = append(workerContainers, state.WorkerContainerNames...)
	for _, container := range workerContainers {
		ip, err := containers.GetContainerIP(container, is)
		if err != nil {
			return err
		}
		workerCertConfig := certs.CertConfig{
			Name:     "node:" + container,
			FileName: container,
			CN:       "system:node:" + container,
			CA: certs.CAConfig{
				Name: "ca",
				Dir:  certDir,
				CN:   "Kubernetes",
			},
			Dir:          certDir,
			CAConfigPath: path.Join(certDir, "ca-config.json"),
			ExtraOpts: map[string]string{
				"hostname": ip + "," + container,
			},
		}

		err = certificates.CreateCert(workerCertConfig)
		if err != nil {
			return err
		}
	}

	// configure etcd
	etcdCertPaths := []string{
		path.Join(certDir, "etcd.pem"),
		path.Join(certDir, "etcd-key.pem"),
		path.Join(certDir, "ca-etcd.pem"),
	}
	err = containers.UploadFiles(etcdCertPaths, "/etc/etcd/", state.EtcdContainerName, is)
	if err != nil {
		return err
	}

	err = containers.UploadFile([]byte("ETCD_IP="+etcdIP), "", "/etc/etcd/env", state.EtcdContainerName, is)
	if err != nil {
		return err
	}

	err = containers.RunCommands(state.EtcdContainerName, []string{
		"systemctl daemon-reload",
		"systemctl -q enable etcd",
		"systemctl start etcd",
	}, is)
	if err != nil {
		return err
	}

	// configure registry
	err = containers.RunCommands(state.RegistryContainerName, []string{
		"systemctl daemon-reload",
		"systemctl -q enable oci-registry",
		"systemctl start oci-registry",
	}, is)
	if err != nil {
		return err
	}

	// configure controller
	kfgPath := path.Join(cacheDir, state.Name, "kubeconfigs")
	err = os.MkdirAll(kfgPath, 0755)
	if err != nil {
		return fmt.Errorf("could not mkdir %s: %w", kfgPath, err)
	}

	err = containers.UploadFile([]byte("ETCD_IP="+etcdIP), "", "/etc/lxdk/env", state.ControllerContainerName, is)
	if err != nil {
		return err
	}

	err = createControllerKubeconfig(state.ControllerContainerName, path.Join(cacheDir, state.Name), is)
	if err != nil {
		return err
	}

	controllerCertPaths := []string{
		path.Join(certDir, "kubernetes.pem"),
		path.Join(certDir, "kubernetes-key.pem"),
		path.Join(certDir, "ca.pem"),
		path.Join(certDir, "ca-key.pem"),
		path.Join(certDir, "etcd.pem"),
		path.Join(certDir, "etcd-key.pem"),
		path.Join(certDir, "ca-etcd.pem"),
		path.Join(certDir, "ca-aggregation.pem"),
		path.Join(certDir, "aggregation-client.pem"),
		path.Join(certDir, "aggregation-client-key.pem"),
	}
	err = containers.UploadFiles(controllerCertPaths, "/etc/kubernetes/", state.ControllerContainerName, is)
	if err != nil {
		return err
	}

	kfgPaths := []string{
		path.Join(kfgPath, "kube-controller-manager.kubeconfig"),
		path.Join(kfgPath, "kube-scheduler.kubeconfig"),
	}
	err = containers.UploadFiles(kfgPaths, "/etc/kubernetes/", state.ControllerContainerName, is)
	if err != nil {
		return err
	}

	err = containers.RunCommands(state.ControllerContainerName, []string{
		"systemctl daemon-reload",
		"systemctl -q enable kube-apiserver",
		"systemctl start kube-apiserver",
		"systemctl -q enable kube-controller-manager",
		"systemctl start kube-controller-manager",
		"systemctl -q enable kube-scheduler",
		"systemctl start kube-scheduler",
	}, is)
	if err != nil {
		return err
	}

	// configure controller as worker
	// configure worker(s)
	registryIP, err := containers.GetContainerIP(state.RegistryContainerName, is)
	if err != nil {
		return err
	}

	for _, worker := range workerContainers {
		containerConfig := workerConfig{
			ContainerName: worker,
			ControllerIP:  controllerIP,
			RegistryName:  state.RegistryContainerName,
			RegistryIP:    registryIP,
			EtcdIP:        etcdIP,
			ClusterDir:    path.Join(cacheDir, state.Name),
		}
		err = configureWorker(containerConfig, is)
		if err != nil {
			return err
		}
	}

	// create admin kubeconfig
	err = createAdminKubeconfig(path.Join(cacheDir, state.Name), controllerIP)
	if err != nil {
		return err
	}

	clientset, err := kubernetes.GetClientset(path.Join(cacheDir, state.Name, "kubeconfigs", "admin.kubeconfig"))
	if err != nil {
		return err
	}

	err = kubernetes.WaitAPIServerReady(*clientset)
	if err != nil {
		return err
	}

	err = kubernetes.ConfigureRBAC(*clientset)
	if err != nil {
		return err
	}

	// flannel
	err = kubernetes.DeployManifest(path.Join(cacheDir, state.Name), kubernetes.FlannelManifest())
	if err != nil {
		return err
	}

	// core dns
	err = kubernetes.DeployManifest(path.Join(cacheDir, state.Name), kubernetes.CoreDNSManifest())
	if err != nil {
		return err
	}

	return nil
}

func createControllerKubeconfig(container, clusterDir string, is lxdclient.InstanceServer) error {
	ip, err := containers.GetContainerIP(container, is)
	if err != nil {
		return err
	}

	// TODO: pure go?
	out, err := exec.Command("kubectl",
		"config",
		"set-cluster",
		"lxdk",
		"--certificate-authority="+path.Join(clusterDir, "certificates", "ca.pem"),
		"--embed-certs=true",
		"--server=https://"+ip+":6443",
		"--kubeconfig="+path.Join(clusterDir, "kubeconfigs", "kube-controller-manager.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-cluster': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"set-credentials",
		"kube-controller-manager",
		"--client-certificate="+path.Join(clusterDir, "certificates", "kube-controller-manager.pem"),
		"--client-key="+path.Join(clusterDir, "certificates", "kube-controller-manager-key.pem"),
		"--embed-certs=true",
		"--kubeconfig="+path.Join(clusterDir, "kubeconfigs", "kube-controller-manager.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-credentials': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"set-context",
		"default",
		"--cluster=lxdk",
		"--user=kube-controller-manager",
		"--kubeconfig="+path.Join(clusterDir, "kubeconfigs", "kube-controller-manager.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-context': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"use-context",
		"default",
		"--kubeconfig="+path.Join(clusterDir, "kubeconfigs", "kube-controller-manager.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl use-context': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config", "set-cluster",
		"lxdk",
		"--certificate-authority="+path.Join(clusterDir, "certificates", "ca.pem"),
		"--embed-certs=true",
		"--kubeconfig="+path.Join(clusterDir, "kubeconfigs", "kube-scheduler.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-cluster': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"set-credentials",
		"kube-scheduler",
		"--client-certificate="+path.Join(clusterDir, "certificates", "kube-scheduler.pem"),
		"--client-key="+path.Join(clusterDir, "certificates", "kube-scheduler-key.pem"),
		"--embed-certs=true",
		"--kubeconfig="+path.Join(clusterDir, "kubeconfigs", "kube-scheduler.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-credentials': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"set-context",
		"default",
		"--cluster=lxdk",
		"--user=kube-scheduler",
		"--kubeconfig="+path.Join(clusterDir, "kubeconfigs", "kube-scheduler.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-context': %s", out)
	}

	return nil
}

// TODO: args should be a struct
type workerConfig struct {
	ContainerName string
	ControllerIP  string
	RegistryName  string
	RegistryIP    string
	EtcdIP        string
	ClusterDir    string
}

func configureWorker(wc workerConfig, is lxdclient.InstanceServer) error {
	var data []byte
	if strings.Contains(wc.ContainerName, "controller") {
		data = []byte(fmt.Sprintf("CONTAINER_NAME=%s\nETCD_IP=%s", wc.ContainerName, wc.EtcdIP))
	} else {
		data = []byte("CONTAINER_NAME=" + wc.ContainerName)
	}

	err := containers.UploadFile(data, "", "/etc/lxdk/env", wc.ContainerName, is)
	if err != nil {
		return err
	}

	err = createWorkerKubeconfig(wc.ContainerName, wc.ControllerIP, wc.ClusterDir)
	if err != nil {
		return err
	}

	certDir := path.Join(wc.ClusterDir, "certificates")
	kcfgDir := path.Join(wc.ClusterDir, "kubeconfigs")

	workerCertPaths := []string{
		path.Join(certDir, wc.ContainerName+".pem"),
		path.Join(certDir, wc.ContainerName+"-key.pem"),
	}
	err = containers.UploadFiles(workerCertPaths, "/etc/kubernetes/", wc.ContainerName, is)
	if err != nil {
		return err
	}

	workerKcfgPaths := []string{
		path.Join(kcfgDir, "kube-proxy.kubeconfig"),
		path.Join(kcfgDir, wc.ContainerName+"-kubelet.kubeconfig"),
	}
	err = containers.UploadFiles(workerKcfgPaths, "/etc/kubernetes/", wc.ContainerName, is)
	if err != nil {
		return err
	}

	in, _, err := is.GetInstance(wc.ContainerName)
	if err != nil {
		return err
	}

	var newDevices api.InstancePut
	newDevices = in.InstancePut

	entries, err := os.ReadDir("/dev/")
	if err != nil {
		return fmt.Errorf("could read dir /dev: %w", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "loop") && len(entry.Name()) == 5 {
			newDevices.Devices[entry.Name()] = map[string]string{
				"type":   "unix-block",
				"source": path.Join("/dev", entry.Name()),
				"path":   path.Join("/dev", entry.Name()),
			}
		}
	}

	for _, dev := range []string{"kvm", "net/tun", "vhost-net", "vhost-vsock", "vsock"} {
		newDevices.Devices[strings.ReplaceAll(dev, "/", "-")] = map[string]string{
			"type":   "unix-char",
			"source": path.Join("/dev", dev),
			"path":   path.Join("/dev", dev),
		}
	}
	newDevices.Devices["vhost-scsi"] = map[string]string{
		"type":   "unix-char",
		"source": path.Join("/dev", "vhost-scsi"),
		"path":   path.Join("/dev", "vhost-sci"),
	}

	op, err := is.UpdateInstance(wc.ContainerName, newDevices, "")
	if err != nil {
		return err
	}

	err = op.Wait()
	if err != nil {
		return err
	}

	err = containers.RunCommands(wc.ContainerName, []string{
		"mkdir -p /etc/containers",
		"mkdir -p /usr/share/containers/oci/hooks.d",
		"ln -s /etc/crio/policy.json /etc/containers/policy.json",
		"mkdir -p /etc/cni/net.d",
		"mkdir -p /etc/kubernetes/config",
		"ln -s /dev/console /dev/kmsg",
	}, is)
	if err != nil {
		return err
	}

	registryConf := workerRegistriesConfig(wc.RegistryName, wc.RegistryIP)
	err = containers.UploadFile(registryConf, "", "/etc/containers/registries.conf", wc.ContainerName, is)
	if err != nil {
		return nil
	}

	kubeletConf := kubeletConfig(wc.ContainerName)
	err = containers.UploadFile(kubeletConf, "", "/etc/kubernetes/config/kubelet.yaml", wc.ContainerName, is)
	if err != nil {
		return nil
	}

	kubeletUnit := kubeletUnitConfig(wc.ContainerName)
	err = containers.UploadFile(kubeletUnit, "", "/etc/systemd/system/kubelet.service", wc.ContainerName, is)
	if err != nil {
		return nil
	}

	err = containers.RunCommands(wc.ContainerName, []string{
		"systemctl daemon-reload",
		"systemctl -q enable crio",
		"systemctl start crio",
		"systemctl -q enable kubelet",
		"systemctl start kubelet",
		"systemctl -q enable kube-proxy",
		"systemctl start kube-proxy",
	}, is)
	if err != nil {
		return err
	}

	return nil
}

func createWorkerKubeconfig(container, controllerIP, clusterDir string) error {
	certDir := path.Join(clusterDir, "certificates")
	kfgDir := path.Join(clusterDir, "kubeconfigs")

	out, err := exec.Command("kubectl",
		"config",
		"set-cluster",
		"kubedee",
		"--certificate-authority="+path.Join(certDir, "ca.pem"),
		"--embed-certs=true",
		"--server=https://"+controllerIP+":6443",
		"--kubeconfig="+path.Join(kfgDir, "kube-proxy.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-cluster': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"set-credentials",
		"kube-proxy",
		"--client-certificate="+path.Join(certDir, "kube-proxy.pem"),
		"--client-key="+path.Join(certDir, "kube-proxy-key.pem"),
		"--embed-certs=true",
		"--kubeconfig="+path.Join(kfgDir, "kube-proxy.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-credentials': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"set-context",
		"default",
		"--cluster=kubedee",
		"--user=kube-proxy",
		"--kubeconfig="+path.Join(kfgDir, "kube-proxy.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-context': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"use-context",
		"default",
		"--kubeconfig="+path.Join(kfgDir, "kube-proxy.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl use-context': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"set-cluster",
		"kubedee",
		"--certificate-authority="+path.Join(certDir, "ca.pem"),
		"--embed-certs=true",
		"--server=https://"+controllerIP+":6443",
		"--kubeconfig="+path.Join(kfgDir, container+"-kubelet.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-cluster': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"set-credentials",
		"system:node:"+container,
		"--client-certificate="+path.Join(certDir, container+".pem"),
		"--client-key="+path.Join(certDir, container+"-key.pem"),
		"--embed-certs=true",
		"--kubeconfig="+path.Join(kfgDir, container+"-kubelet.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-credentials': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"set-context",
		"default",
		"--cluster=kubedee",
		"--user=system:node:"+container,
		"--kubeconfig="+path.Join(kfgDir, container+"-kubelet.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-context': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"use-context",
		"default",
		"--kubeconfig="+path.Join(kfgDir, container+"-kubelet.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl use-context': %s", out)
	}

	return nil
}

// TODO: do these with env files and not like this
func workerRegistriesConfig(registryName, registryIP string) []byte {
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

func kubeletConfig(containerName string) []byte {
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

func kubeletUnitConfig(containerName string) []byte {
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

func createAdminKubeconfig(clusterDir, controllerIP string) error {
	certDir := path.Join(clusterDir, "certificates")
	kfgDir := path.Join(clusterDir, "kubeconfigs")

	out, err := exec.Command("kubectl",
		"config",
		"set-cluster",
		"kubedee",
		"--certificate-authority="+path.Join(certDir, "ca.pem"),
		"--embed-certs=true",
		"--server=https://"+controllerIP+":6443",
		"--kubeconfig="+path.Join(kfgDir, "admin.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-cluster': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"set-credentials",
		"admin",
		"--client-certificate="+path.Join(certDir, "admin.pem"),
		"--client-key="+path.Join(certDir, "admin-key.pem"),
		"--embed-certs=true",
		"--kubeconfig="+path.Join(kfgDir, "admin.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-credentials': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"set-context",
		"default",
		"--cluster=kubedee",
		"--user=admin",
		"--kubeconfig="+path.Join(kfgDir, "admin.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-context': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"use-context",
		"default",
		"--kubeconfig="+path.Join(kfgDir, "admin.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl use-context': %s", out)
	}

	return nil
}
