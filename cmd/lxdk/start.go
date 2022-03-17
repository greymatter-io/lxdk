package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"
	"syscall"

	"github.com/greymatter-io/lxdk/certificates"
	certs "github.com/greymatter-io/lxdk/certificates"
	"github.com/greymatter-io/lxdk/config"
	"github.com/greymatter-io/lxdk/containers"
	"github.com/greymatter-io/lxdk/lxd"
	lxdclient "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/urfave/cli/v2"
)

var startCmd = &cli.Command{
	Name:   "start",
	Usage:  "start a cluster",
	Action: doStart,
}

// TODO: apiserver flags should be configurable, use a .env file for
// the apiserver systemd service
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
		err = containers.StartContainer(container, is)
		if err != nil {
			return err
		}

	}

	var etcdContainerName string
	var controllerContainerName string
	var registryContainerName string
	var workerContainerNames []string
	for _, container := range state.Containers {
		ip, err := containers.GetContainerIP(container, is)
		if err != nil {
			return err
		}

		// registry
		if strings.Contains(container, "registry") {
			registryContainerName = container
		}

		// etcd cert
		if strings.Contains(container, "etcd") {
			etcdContainerName = container
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
					"hostname": ip + ",127.0.0.1",
				},
			}

			err = certificates.CreateCert(etcdCertConfig)
			if err != nil {
				return err
			}
		}

		// conroller cert
		if strings.Contains(container, "controller") {
			controllerContainerName = container
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
					"hostname": ip + ",127.0.0.1",
				},
			}

			err = certificates.CreateCert(controllerCertConfig)
			if err != nil {
				return err
			}
		}

		// TODO: probably going to have to move this out
		// worker cert
		if strings.Contains(container, "worker") || strings.Contains(container, "controller") {
			workerContainerNames = append(workerContainerNames, container)
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
	}

	// configure etcd
	etcdCertPaths := []string{
		path.Join(certDir, "etcd.pem"),
		path.Join(certDir, "etcd-key.pem"),
		path.Join(certDir, "ca-etcd.pem"),
	}
	err = uploadFiles(etcdCertPaths, "/etc/etcd/", etcdContainerName, is)
	if err != nil {
		return err
	}

	err = runCommands(etcdContainerName, []string{
		"systemctl daemon-reload",
		"systemctl -q enable etcd",
		"systemctl start etcd",
	}, is)
	if err != nil {
		return err
	}

	// configure registry
	err = runCommands(registryContainerName, []string{
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

	err = createControllerKubeconfig(controllerContainerName, path.Join(cacheDir, state.Name), is)
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
	err = uploadFiles(controllerCertPaths, "/etc/kubernetes/", controllerContainerName, is)
	if err != nil {
		return err
	}

	kfgPaths := []string{
		path.Join(kfgPath, "kube-controller-manager.kubeconfig"),
		path.Join(kfgPath, "kube-scheduler.kubeconfig"),
	}
	err = uploadFiles(kfgPaths, "/etc/kubernetes/", controllerContainerName, is)
	if err != nil {
		return err
	}

	err = runCommands(controllerContainerName, []string{
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
	controllerIP, err := containers.GetContainerIP(controllerContainerName, is)
	if err != nil {
		return err
	}

	registryIP, err := containers.GetContainerIP(registryContainerName, is)
	if err != nil {
		return err
	}

	for _, worker := range workerContainerNames {
		// TODO: make the args a struct
		err = configureWorker(worker, controllerIP, registryContainerName, registryIP, path.Join(cacheDir, state.Name), is)
		if err != nil {
			return err
		}
	}

	// create admin kubeconfig
	// configure rbac

	return nil
}

// from is a file, to is a dir
func uploadFile(data []byte, from, to, container string, is lxdclient.InstanceServer) error {
	var UID int64
	var GID int64
	var mode os.FileMode
	if data == nil || len(data) == 0 {
		stat, err := os.Stat(from)
		if err != nil {
			return fmt.Errorf("cannot stat %s: %w", from, err)
		}

		if linuxstat, ok := stat.Sys().(*syscall.Stat_t); ok {
			UID = int64(linuxstat.Uid)
			GID = int64(linuxstat.Gid)
		} else {
			UID = int64(os.Getuid())
			GID = int64(os.Getgid())
		}
		mode = os.FileMode(0755)

		data, err = ioutil.ReadFile(from)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", from, err)
		}

	} else {
		UID = int64(os.Getuid())
		GID = int64(os.Getgid())
		mode = os.FileMode(0755)
	}

	reader := bytes.NewReader(data)

	args := lxdclient.InstanceFileArgs{
		Type:    "file",
		UID:     UID,
		GID:     GID,
		Mode:    int(mode.Perm()),
		Content: reader,
	}
	_, filename := path.Split(from)
	toPath := path.Join(to, filename)

	err := recursiveMkDir(container, to, mode, UID, GID, is)
	if err != nil {
		return err
	}

	err = is.CreateInstanceFile(container, toPath, args)
	if err != nil {
		return fmt.Errorf("cannot push %s to %s: %w", from, toPath, err)
	}

	return nil
}

func uploadFiles(froms []string, to, container string, is lxdclient.InstanceServer) error {
	for _, from := range froms {
		err := uploadFile(nil, from, to, container, is)
		if err != nil {
			return err
		}
	}
	return nil
}

func recursiveMkDir(container, dir string, mode os.FileMode, UID, GID int64, is lxdclient.InstanceServer) error {
	if dir == "/" {
		return nil
	}

	if strings.HasSuffix(dir, "/") {
		dir = dir[:len(dir)-1]
	}

	split := strings.Split(dir[1:], "/")
	if len(split) > 1 {
		err := recursiveMkDir(container, "/"+strings.Join(split[:len(split)-1], "/"), mode, UID, GID, is)
		if err != nil {
			return err
		}
	}

	_, resp, err := is.GetInstanceFile(container, dir)
	if err == nil && resp.Type == "directory" {
		return nil
	}
	if err == nil && resp.Type != "directory" {
		return fmt.Errorf("%s is not a directory", dir)
	}

	args := lxdclient.InstanceFileArgs{
		Type: "directory",
		UID:  UID,
		GID:  UID,
		Mode: int(mode.Perm()),
	}
	return is.CreateInstanceFile(container, dir, args)
}

func runCommand(container, command string, is lxdclient.InstanceServer) error {
	split := strings.Split(command, " ")

	op, err := is.ExecInstance(container, api.InstanceExecPost{
		Command: split,
	}, &lxdclient.InstanceExecArgs{})
	if err != nil {
		return err
	}

	err = op.Wait()
	if err != nil {
		return fmt.Errorf("could not run command %s: %w", command, err)
	}

	return nil
}

func runCommands(container string, commands []string, is lxdclient.InstanceServer) error {
	for _, command := range commands {
		err := runCommand(container, command, is)
		if err != nil {
			return err
		}
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
		"config",
		"set-cluster",
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

func configureWorker(container, controllerIP, registryName, registryIP, clusterDir string, is lxdclient.InstanceServer) error {
	err := createWorkerKubeconfig(container, controllerIP, clusterDir)
	if err != nil {
		return err
	}

	certDir := path.Join(clusterDir, "certificates")
	kcfgDir := path.Join(clusterDir, "kubeconfigs")

	workerCertPaths := []string{
		path.Join(certDir, container+".pem"),
		path.Join(certDir, container+"-key.pem"),
	}
	err = uploadFiles(workerCertPaths, "/etc/kubernetes/", container, is)
	if err != nil {
		return err
	}

	workerKcfgPaths := []string{
		path.Join(kcfgDir, "kube-proxy.kubeconfig"),
		path.Join(kcfgDir, container+"-kubelet.kubeconfig"),
	}
	err = uploadFiles(workerKcfgPaths, "/etc/kubernetes/", container, is)
	if err != nil {
		return err
	}

	in, _, err := is.GetInstance(container)
	if err != nil {
		return err
	}

	var newDevices api.InstancePut
	newDevices.Devices = in.Devices

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

	for _, dev := range []string{"kmsg", "kvm", "net/tun", "vhost-net", "vhost-vsock", "vsock"} {
		newDevices.Devices[dev] = map[string]string{
			"type":   "unix-char",
			"source": path.Join("/dev", dev),
			"path":   path.Join("/dev", dev),
		}
	}
	//newDevices.Devices["vhost-scsi"] = map[string]string{
	//"type":   "unix-char",
	//"source": path.Join("/dev", "vhost-scsi"),
	//"path":   path.Join("/dev", "vhost-sci"),
	//}

	op, err := is.UpdateInstance(container, newDevices, "")
	if err != nil {
		return err
	}

	err = op.Wait()
	if err != nil {
		return err
	}

	err = runCommands(container, []string{
		"mkdir -p /etc/containers",
		"mkdir -p /usr/share/containers/oci/hooks.d",
		"ln -s /etc/crio/policy.json /etc/containers/policy.json",
		"mkdir -p /etc/cni/net.d",
		"mkdir -p /etc/kubernetes/config",
	}, is)
	if err != nil {
		return err
	}

	registryConf := workerRegistriesConfig(registryName, registryIP)
	err = uploadFile(registryConf, "", "/etc/containers/registries.conf", container, is)
	if err != nil {
		return nil
	}

	kubeletConf := kubeletConfig(container)
	err = uploadFile(kubeletConf, "", "/etc/kubernetes/config/kubelet.yaml", container, is)
	if err != nil {
		return nil
	}

	kubeletUnit := kubeletUnitConfig(container)
	err = uploadFile(kubeletUnit, "", "/etc/systemd/system/kubelet.service", container, is)
	if err != nil {
		return nil
	}

	err = runCommands(container, []string{
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
ExecStart=/usr/local/bin/kubelet \\
  --config=/etc/kubernetes/config/kubelet.yaml \\
  --container-runtime=remote \\
  --container-runtime-endpoint=unix:///var/run/crio/crio.sock \\
  --image-service-endpoint=unix:///var/run/crio/crio.sock \\
  --kubeconfig=/etc/kubernetes/%s-kubelet.kubeconfig \\
  --register-node=true \\
  --v=2
Restart=on-failure
RestartSec=5
[Install]
WantedBy=multi-user.target`, containerName))
}
