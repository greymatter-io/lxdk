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
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "use-remote-ip",
				Usage: "use the IP of the lxc remote to access the API instead of the controller container",
				Value: false,
			},
		},
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

	if state.RunState == config.Running {
		return fmt.Errorf("cluster %s is already running or was not stopped by lxdk", clusterName)
	}

	is, hostname, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

	for _, containerName := range state.Containers {
		state, _, err := is.GetInstanceState(containerName)
		if err != nil {
			return err
		}

		if state.Status == "Running" {
			return fmt.Errorf("container %s is already running", containerName)
		}
	}

	for _, container := range state.Containers {
		log.Default().Println("starting " + container)
		err = containers.StartContainer(container, is)
		if err != nil {
			return err
		}

	}

	// etcd cert
	etcdIP, err := containers.WaitContainerIP(state.EtcdContainerName, []string{hostname}, is)
	if err != nil {
		return fmt.Errorf("error getting container IP: %w", err)
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
			"hostname": etcdIP.String() + ",127.0.0.1," + hostname,
		},
	}

	err = certificates.CreateCert(etcdCertConfig)
	if err != nil {
		return fmt.Errorf("error creating cert: %w", err)
	}

	controllerIP, err := containers.WaitContainerIP(state.ControllerContainerName, []string{hostname}, is)
	if err != nil {
		return fmt.Errorf("error getting container IP: %w", err)
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
			"hostname": "10.32.0.1," + controllerIP.String() + ",127.0.0.1," + hostname,
		},
	}

	err = certificates.CreateCert(controllerCertConfig)
	if err != nil {
		return err
	}

	// worker cert
	workerContainers := state.WorkerContainerNames
	workerContainers = append(workerContainers, state.ControllerContainerName)
	for _, container := range workerContainers {
		if err = createWorkerCert(container, certDir, hostname, is); err != nil {
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

	err = containers.UploadFile([]byte("ETCD_IP="+etcdIP.String()), "", "/etc/etcd/env", state.EtcdContainerName, is)
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
	if state.RegistryEnabled {
		err = containers.RunCommands(state.RegistryContainerName, []string{
			"systemctl daemon-reload",
			"systemctl -q enable oci-registry",
			"systemctl start oci-registry",
		}, is)
		if err != nil {
			return err
		}
	}

	// configure controller
	kfgPath := path.Join(cacheDir, state.Name, "kubeconfigs")
	err = os.MkdirAll(kfgPath, 0755)
	if err != nil {
		return fmt.Errorf("could not mkdir %s: %w", kfgPath, err)
	}

	err = containers.UploadFile([]byte("ETCD_IP="+etcdIP.String()), "", "/etc/lxdk/env", state.ControllerContainerName, is)
	if err != nil {
		return err
	}

	err = createControllerKubeconfig(state.ControllerContainerName, path.Join(cacheDir, state.Name), controllerIP.String(), hostname, is)
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
		"ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf",
	}, is)
	if err != nil {
		return err
	}

	in, _, err := is.GetInstance(state.ControllerContainerName)
	if err != nil {
		return err
	}

	// expose Kubernetes API server
	var newDevices api.InstancePut
	newDevices = in.InstancePut

	// if not using our own storage pool, mount the storage device the pool
	// is on, otherwise kubelet can't mount it to get the cni config
	if state.StoragePool != "lxdk-"+state.Name {
		pool, _, err := is.GetStoragePool(state.StoragePool)
		if err != nil {
			return err
		}
		if pool.Driver == "btrfs" {
			if source, ok := pool.Config["source"]; ok {
				// Get the actual mount path for a device if it
				// is configured with a UUID. A slash at the
				// beginning is our heuristic for determining if
				// an lxc source is a path or UUID.
				if !strings.HasPrefix(source, "/") {
					source, err = lxd.GetDeviceByUUID(source)
					if err != nil {
						return err
					}
				}
				newDevices.Devices["btrfsmnt"] = map[string]string{
					"type":   "unix-block",
					"source": source,
					"path":   source,
				}
			}
		}
	}

	op, err := is.UpdateInstance(state.ControllerContainerName, newDevices, "")
	if err != nil {
		return err
	}
	err = op.Wait()
	if err != nil {
		return err
	}

	// configure controller as worker
	// configure worker(s)
	for _, worker := range workerContainers {
		containerConfig := workerConfig{
			ContainerName: worker,
			ControllerIP:  controllerIP.String(),
			EtcdIP:        etcdIP.String(),
			ClusterDir:    path.Join(cacheDir, state.Name),
		}
		if state.RegistryEnabled {
			registryIP, err := containers.WaitContainerIP(state.RegistryContainerName, []string{hostname}, is)
			if err != nil {
				return err
			}
			containerConfig.RegistryName = state.RegistryContainerName
			containerConfig.RegistryIP = registryIP.String()
		}

		err = configureWorker(containerConfig, is)
		if err != nil {
			return err
		}
	}

	// create admin kubeconfig
	err = createAdminKubeconfig(path.Join(cacheDir, state.Name), controllerIP.String())
	if err != nil {
		return err
	}

	if ctx.Bool("use-remote-ip") {
		err = createClientKubeconfig(path.Join(cacheDir, state.Name), hostname)
		if err != nil {
			return err
		}
	} else {
		err = createClientKubeconfig(path.Join(cacheDir, state.Name), controllerIP.String())
		if err != nil {
			return err
		}
	}

	clientset, err := kubernetes.GetClientset(path.Join(cacheDir, state.Name, "kubeconfigs", "client.kubeconfig"))
	if err != nil {
		return err
	}

	log.Default().Println("waiting for API server...")
	err = kubernetes.WaitAPIServerReady(*clientset)
	if err != nil {
		return err
	}

	log.Default().Println("configuring RBAC")
	err = kubernetes.ConfigureRBAC(*clientset)
	if err != nil {
		return err
	}

	// flannel
	log.Default().Println("setting up flannel networking")
	err = kubernetes.DeployManifest(path.Join(cacheDir, state.Name), kubernetes.FlannelManifest())
	if err != nil {
		return err
	}

	// core dns
	log.Default().Println("deploying core DNS")
	err = kubernetes.DeployManifest(path.Join(cacheDir, state.Name), kubernetes.CoreDNSManifest())
	if err != nil {
		return err
	}

	log.Default().Println("waiting for controller node to become ready")
	err = kubernetes.WaitNode(*clientset, state.ControllerContainerName)
	if err != nil {
		return err
	}

	log.Default().Println("labeling and tainting controller node")
	kfg := path.Join(cacheDir, state.Name, "kubeconfigs", "client.kubeconfig")
	// label and taint controller
	err = containers.RunCommands(state.ControllerContainerName, []string{
		fmt.Sprintf(`kubectl --kubeconfig=%s label node %s node-role.kubernetes.io/master=""`, kfg, state.ControllerContainerName),
		fmt.Sprintf(`kubectl --kubeconfig=%s label node %s ingress-nginx=""`, kfg, state.ControllerContainerName),
		fmt.Sprintf(`kubectl --kubeconfig=%s taint node %s node-role.kubernetes.io/master=:NoSchedule`, kfg, state.ControllerContainerName),
	}, is)
	if err != nil {
		return err
	}

	state.RunState = config.Running
	if err := config.WriteClusterState(ctx, state); err != nil {
		return err
	}

	// create serviceaccounts

	return nil
}

func createWorkerCert(worker, certDir, hostname string, is lxdclient.InstanceServer) error {
	ip, err := containers.WaitContainerIP(worker, []string{hostname}, is)
	if err != nil {
		return err
	}
	workerCertConfig := certs.CertConfig{
		Name:     "node:" + strings.ToLower(worker),
		FileName: strings.ToLower(worker),
		CN:       "system:node:" + strings.ToLower(worker),
		CA: certs.CAConfig{
			Name: "ca",
			Dir:  certDir,
			CN:   "Kubernetes",
		},
		Dir:          certDir,
		CAConfigPath: path.Join(certDir, "ca-config.json"),
		ExtraOpts: map[string]string{
			"hostname": ip.String() + "," + worker,
		},
		JSONOverride: certs.CertJSON("system:node:"+strings.ToLower(worker), "system:nodes"),
	}

	return certificates.CreateCert(workerCertConfig)
}

func createControllerKubeconfig(container, clusterDir, controllerIP, hostname string, is lxdclient.InstanceServer) error {
	ip, err := containers.WaitContainerIP(container, []string{hostname}, is)
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
		"--server=https://"+ip.String()+":6443",
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
		"--server=https://"+controllerIP+":6443",
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

	out, err = exec.Command("kubectl",
		"config",
		"use-context",
		"default",
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
	lowerName := strings.ToLower(wc.ContainerName)
	var data []byte
	if strings.Contains(wc.ContainerName, "controller") {
		data = []byte(fmt.Sprintf("CONTAINER_NAME=%s\nETCD_IP=%s", lowerName, wc.EtcdIP))
	} else {
		data = []byte("CONTAINER_NAME=" + lowerName)
	}

	err := containers.UploadFile(data, "", "/etc/lxdk/env", wc.ContainerName, is)
	if err != nil {
		return err
	}

	certDir := path.Join(wc.ClusterDir, "certificates")
	kcfgDir := path.Join(wc.ClusterDir, "kubeconfigs")

	err = createWorkerKubeconfig(lowerName, wc.ControllerIP, wc.ClusterDir)
	if err != nil {
		return err
	}

	workerCertPaths := []string{
		path.Join(certDir, "ca.pem"),
		path.Join(certDir, "ca-key.pem"),
		path.Join(certDir, "etcd.pem"),
		path.Join(certDir, "etcd-key.pem"),
		path.Join(certDir, "ca-etcd.pem"),
		path.Join(certDir, strings.ToLower(wc.ContainerName)+".pem"),
		path.Join(certDir, strings.ToLower(wc.ContainerName)+"-key.pem"),
	}
	err = containers.UploadFiles(workerCertPaths, "/etc/kubernetes/", wc.ContainerName, is)
	if err != nil {
		return err
	}

	workerKcfgPaths := []string{
		path.Join(kcfgDir, "kube-proxy.kubeconfig"),
		path.Join(kcfgDir, strings.ToLower(wc.ContainerName)+"-kubelet.kubeconfig"),
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
	newDevices.Devices["net-tun"] = map[string]string{
		"type":   "unix-char",
		"source": "/dev/net/tun",
		"path":   "/dev/net/tun",
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
		"ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf",
	}, is)
	if err != nil {
		return err
	}

	registryConf := kubernetes.WorkerRegistriesConfig(wc.RegistryName, wc.RegistryIP)
	err = containers.UploadFile(registryConf, "", "/etc/containers/registries.conf", wc.ContainerName, is)
	if err != nil {
		return nil
	}

	kubeletConf := kubernetes.KubeletConfig(lowerName)
	err = containers.UploadFile(kubeletConf, "", "/etc/kubernetes/config/kubelet.yaml", wc.ContainerName, is)
	if err != nil {
		return nil
	}

	kubeletUnit := kubernetes.KubeletUnitConfig(lowerName)
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
		"lxdk",
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
		"--cluster=lxdk",
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
		"lxdk",
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
		"--cluster=lxdk",
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

func createAdminKubeconfig(clusterDir, controllerIP string) error {
	certDir := path.Join(clusterDir, "certificates")
	kfgDir := path.Join(clusterDir, "kubeconfigs")

	out, err := exec.Command("kubectl",
		"config",
		"set-cluster",
		"lxdk",
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
		"--cluster=lxdk",
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

func createClientKubeconfig(clusterDir, remoteIP string) error {
	certDir := path.Join(clusterDir, "certificates")
	kfgDir := path.Join(clusterDir, "kubeconfigs")

	out, err := exec.Command("kubectl",
		"config",
		"set-cluster",
		"lxdk",
		"--certificate-authority="+path.Join(certDir, "ca.pem"),
		"--embed-certs=true",
		"--server=https://"+remoteIP+":6443",
		"--kubeconfig="+path.Join(kfgDir, "client.kubeconfig"),
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
		"--kubeconfig="+path.Join(kfgDir, "client.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-credentials': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"set-context",
		"default",
		"--cluster=lxdk",
		"--user=admin",
		"--kubeconfig="+path.Join(kfgDir, "client.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl set-context': %s", out)
	}

	out, err = exec.Command("kubectl",
		"config",
		"use-context",
		"default",
		"--kubeconfig="+path.Join(kfgDir, "client.kubeconfig"),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error on 'kubectl use-context': %s", out)
	}

	return nil
}
