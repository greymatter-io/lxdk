package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/greymatter-io/lxdk/certificates"
	certs "github.com/greymatter-io/lxdk/certificates"
	"github.com/greymatter-io/lxdk/config"
	"github.com/greymatter-io/lxdk/containers"
	"github.com/greymatter-io/lxdk/lxd"
	lxdclient "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/urfave/cli/v2"
	"golang.org/x/net/context"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rbac "k8s.io/client-go/applyconfigurations/rbac/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	startCmd = &cli.Command{
		Name:   "start",
		Usage:  "start a cluster",
		Action: doStart,
	}

	UID int64
	GID int64
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

		// controller cert
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

	etcdIP, err := containers.GetContainerIP(etcdContainerName, is)
	if err != nil {
		return err
	}

	err = uploadFile([]byte("ETCD_IP="+etcdIP), "", "/etc/etcd/env", etcdContainerName, is)
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

	err = uploadFile([]byte("ETCD_IP="+etcdIP), "", "/etc/lxdk/env", controllerContainerName, is)
	if err != nil {
		return err
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
		err = configureWorker(worker, controllerIP, registryContainerName, registryIP, etcdIP, path.Join(cacheDir, state.Name), is)
		if err != nil {
			return err
		}
	}

	// create admin kubeconfig
	err = createAdminKubeconfig(path.Join(cacheDir, state.Name), controllerIP)
	if err != nil {
		return err
	}

	adminKfg, err := clientcmd.BuildConfigFromFlags("", path.Join(cacheDir, state.Name, "kubeconfigs", "admin.kubeconfig"))
	if err != nil {
		return err
	}

	clientset, err := kubernetes.NewForConfig(adminKfg)
	if err != nil {
		return err
	}

	err = waitAPIServerReady(*clientset)
	if err != nil {
		return err
	}

	err = configureRBAC(*clientset)
	if err != nil {
		return err
	}

	// flannel
	err = deployManifest(path.Join(cacheDir, state.Name), flannelManifest())
	if err != nil {
		return err
	}

	// core dns
	err = deployManifest(path.Join(cacheDir, state.Name), coreDNSManifest())
	if err != nil {
		return err
	}

	return nil
}

// from is a file, to is a dir
func uploadFile(data []byte, from, to, container string, is lxdclient.InstanceServer) error {
	var mode os.FileMode
	var toPath string
	// if data does not exist, read a file from disk and to should be a
	// directory
	if data == nil || len(data) == 0 {
		stat, err := os.Stat(from)
		if err != nil {
			return fmt.Errorf("cannot stat %s: %w", from, err)
		}

		if linuxstat, ok := stat.Sys().(*syscall.Stat_t); ok {
			UID = int64(linuxstat.Uid)
			GID = int64(linuxstat.Gid)
		}
		mode = os.FileMode(0755)

		data, err = ioutil.ReadFile(from)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", from, err)
		}
		_, filename := path.Split(from)
		toPath = path.Join(to, filename)

		err = recursiveMkDir(container, to, mode, UID, GID, is)
		if err != nil {
			return err
		}
	} else {
		// if data exists, to should be a filename and we have to
		// let lxc infer the UID and GID
		toPath = to
		mode = os.FileMode(0755)

		toDir, _ := path.Split(to)
		err := recursiveMkDir(container, toDir, mode, UID, GID, is)
		if err != nil {
			return err
		}
	}

	reader := bytes.NewReader(data)

	args := lxdclient.InstanceFileArgs{
		Type:    "file",
		UID:     UID,
		GID:     GID,
		Mode:    int(mode.Perm()),
		Content: reader,
	}

	err := is.CreateInstanceFile(container, toPath, args)
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

// TODO: args should be a struct
func configureWorker(container, controllerIP, registryName, registryIP, etcdIP, clusterDir string, is lxdclient.InstanceServer) error {
	var data []byte
	if strings.Contains(container, "controller") {
		data = []byte(fmt.Sprintf("CONTAINER_NAME=%s\nETCD_IP=%s", container, etcdIP))
	} else {
		data = []byte("CONTAINER_NAME=" + container)
	}

	err := uploadFile(data, "", "/etc/lxdk/env", container, is)
	if err != nil {
		return err
	}

	err = createWorkerKubeconfig(container, controllerIP, clusterDir)
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
		"ln -s /dev/console /dev/kmsg",
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

func waitAPIServerReady(clientset kubernetes.Clientset) error {
	_, err := clientset.RbacV1().ClusterRoles().Get(context.Background(), "", v1.GetOptions{})
	for c := 0; c < 50 && err != nil; c++ {
		time.Sleep(3)
	}

	return nil
}

func configureRBAC(clientset kubernetes.Clientset) error {
	apiVersion := "rbac.authorization.k8s.io/v1"

	apiToKubelet := rbac.ClusterRole("system:kube-apiserver-to-kubelet")
	apiToKubelet.APIVersion = &apiVersion
	apiToKubelet.Annotations = map[string]string{
		"rbac.authorization.kubernetes.io/autoupdate": "true",
	}
	apiToKubelet.Labels = map[string]string{
		"kubernetes.io/bootstrapping": "rbac-defaults",
	}
	apiToKubelet.Rules = []rbac.PolicyRuleApplyConfiguration{
		{
			APIGroups: []string{""},
			Resources: []string{
				"nodes/proxy",
				"nodes/stats",
				"nodes/log",
				"nodes/spec",
				"nodes/metrics",
			},
			Verbs: []string{"*"},
		},
	}

	_, err := clientset.RbacV1().ClusterRoles().Apply(context.Background(), apiToKubelet, v1.ApplyOptions{
		FieldManager: "application/apply-patch",
	})
	if err != nil {
		return fmt.Errorf("could not create cluster role: %w", err)
	}

	kubeAPIServer := rbac.ClusterRoleBinding("system:kube-apiserver")
	roleRefAPIGroup := "rbac.authorization.k8s.io"
	roleRefKind := "ClusterRole"
	roleRefName := "system:kube-apiserver-to-kubelet"
	roleRef := rbac.RoleRefApplyConfiguration{
		APIGroup: &roleRefAPIGroup,
		Kind:     &roleRefKind,
		Name:     &roleRefName,
	}

	namespace := ""
	apiToKubelet.APIVersion = &apiVersion
	kubeAPIServer.Namespace = &namespace
	kubeAPIServer.RoleRef = &roleRef

	subjectsAPIGroup := "rbac.authorization.k8s.io"
	subjectsKind := "User"
	subjectsName := "Kubernetes"

	kubeAPIServer.Subjects = []rbac.SubjectApplyConfiguration{
		{
			APIGroup: &subjectsAPIGroup,
			Kind:     &subjectsKind,
			Name:     &subjectsName,
		},
	}

	return nil
}

func deployManifest(clusterDir string, data []byte) error {
	kfg := path.Join(clusterDir, "kubeconfigs", "admin.kubeconfig")

	cmd := exec.Command("kubectl", "apply", "--kubeconfig", kfg, "-f", "-")

	pipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("error creating stdin pipe: %w", err)
	}

	_, err = pipe.Write(data)
	if err != nil {
		return fmt.Errorf("error writing to pipe: %w", err)
	}
	err = pipe.Close()
	if err != nil {
		return fmt.Errorf("error closing pipe: %w", err)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Default().Println(out)
		return err
	}

	return nil
}

// TODO: WOW This is ugly, need to either gut it out and write the go for it or
// check it in another way
func flannelManifest() []byte {
	return []byte(`---
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: psp.flannel.unprivileged
  annotations:
    seccomp.security.alpha.kubernetes.io/allowedProfileNames: docker/default
    seccomp.security.alpha.kubernetes.io/defaultProfileName: docker/default
    apparmor.security.beta.kubernetes.io/allowedProfileNames: runtime/default
    apparmor.security.beta.kubernetes.io/defaultProfileName: runtime/default
spec:
  privileged: false
  volumes:
  - configMap
  - secret
  - emptyDir
  - hostPath
  allowedHostPaths:
  - pathPrefix: "/etc/cni/net.d"
  - pathPrefix: "/etc/kube-flannel"
  - pathPrefix: "/run/flannel"
  readOnlyRootFilesystem: false
  # Users and groups
  runAsUser:
    rule: RunAsAny
  supplementalGroups:
    rule: RunAsAny
  fsGroup:
    rule: RunAsAny
  # Privilege Escalation
  allowPrivilegeEscalation: false
  defaultAllowPrivilegeEscalation: false
  # Capabilities
  allowedCapabilities: ['NET_ADMIN']
  defaultAddCapabilities: []
  requiredDropCapabilities: []
  # Host namespaces
  hostPID: false
  hostIPC: false
  hostNetwork: true
  hostPorts:
  - min: 0
    max: 65535
  # SELinux
  seLinux:
    # SELinux is unused in CaaSP
    rule: 'RunAsAny'
---
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: flannel
rules:
- apiGroups: ['extensions']
  resources: ['podsecuritypolicies']
  verbs: ['use']
  resourceNames: ['psp.flannel.unprivileged']
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get"]
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["list", "watch"]
- apiGroups: [""]
  resources: ["nodes/status"]
  verbs: ["patch"]
---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: flannel
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: flannel
subjects:
- kind: ServiceAccount
  name: flannel
  namespace: kube-system
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: flannel
  namespace: kube-system
---
kind: ConfigMap
apiVersion: v1
metadata:
  name: kube-flannel-cfg
  namespace: kube-system
  labels:
    tier: node
    app: flannel
data:
  cni-conf.json: |
    {
      "name": "cbr0",
      "cniVersion": "0.3.1",
      "plugins": [
        {
          "type": "flannel",
          "delegate": {
            "hairpinMode": true,
            "isDefaultGateway": true
          }
        },
        {
          "type": "portmap",
          "capabilities": {
            "portMappings": true
          }
        }
      ]
    }
  net-conf.json: |
    {
      "Network": "10.244.0.0/16",
      "Backend": {
        "Type": "vxlan",
        "DirectRouting": true
      }
    }
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: kube-flannel-ds
  namespace: kube-system
  labels:
    tier: node
    app: flannel
spec:
  selector:
    matchLabels:
      app: flannel
  template:
    metadata:
      labels:
        tier: node
        app: flannel
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: beta.kubernetes.io/os
                operator: In
                values:
                - linux
      hostNetwork: true
      tolerations:
      - operator: Exists
        effect: NoSchedule
      serviceAccountName: flannel
      initContainers:
      - name: install-cni
        image: quay.io/coreos/flannel:v0.15.1
        command:
        - cp
        args:
        - -f
        - /etc/kube-flannel/cni-conf.json
        - /etc/cni/net.d/10-flannel.conflist
        volumeMounts:
        - name: cni
          mountPath: /etc/cni/net.d
        - name: flannel-cfg
          mountPath: /etc/kube-flannel/
      containers:
      - name: kube-flannel
        image: quay.io/coreos/flannel:v0.15.1
        command:
        - /opt/bin/flanneld
        args:
        - --ip-masq
        - --kube-subnet-mgr
        resources:
          requests:
            cpu: "100m"
            memory: "50Mi"
          limits:
            cpu: "100m"
            memory: "50Mi"
        securityContext:
          privileged: false
          capabilities:
            add: ["NET_ADMIN", "NET_RAW"]
        env:
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        volumeMounts:
        - name: run
          mountPath: /run/flannel
        - name: flannel-cfg
          mountPath: /etc/kube-flannel/
      volumes:
      - name: run
        hostPath:
          path: /run/flannel
      - name: cni
        hostPath:
          path: /etc/cni/net.d
      - name: flannel-cfg
        configMap:
          name: kube-flannel-cfg`)
}

func coreDNSManifest() []byte {
	return []byte(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: coredns
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  labels:
    kubernetes.io/bootstrapping: rbac-defaults
  name: system:coredns
rules:
- apiGroups:
  - ""
  resources:
  - endpoints
  - services
  - pods
  - namespaces
  verbs:
  - list
  - watch
- apiGroups:
  - ""
  resources:
  - nodes
  verbs:
  - get
- apiGroups:
  - discovery.k8s.io
  resources:
  - endpointslices
  verbs:
  - list
  - watch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  annotations:
    rbac.authorization.kubernetes.io/autoupdate: "true"
  labels:
    kubernetes.io/bootstrapping: rbac-defaults
  name: system:coredns
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:coredns
subjects:
- kind: ServiceAccount
  name: coredns
  namespace: kube-system
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: coredns
  namespace: kube-system
data:
  Corefile: |
    .:53 {
        errors
        health
        kubernetes cluster.local in-addr.arpa ip6.arpa {
          pods insecure
          fallthrough in-addr.arpa ip6.arpa
        }
        prometheus :9153
        forward . /etc/resolv.conf
        cache 30
        loop
        reload
        loadbalance
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: coredns
  namespace: kube-system
  labels:
    k8s-app: kube-dns
    kubernetes.io/name: "CoreDNS"
spec:
  replicas: 2
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
  selector:
    matchLabels:
      k8s-app: kube-dns
  template:
    metadata:
      labels:
        k8s-app: kube-dns
    spec:
      priorityClassName: system-cluster-critical
      serviceAccountName: coredns
      tolerations:
        - key: "CriticalAddonsOnly"
          operator: "Exists"
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: beta.kubernetes.io/os
                operator: In
                values:
                - linux
            - matchExpressions:
              - key: kubernetes.io/os
                operator: In
                values:
                - linux
      containers:
      - name: coredns
        image: coredns/coredns
        imagePullPolicy: IfNotPresent
        resources:
          limits:
            memory: 170Mi
          requests:
            cpu: 100m
            memory: 70Mi
        args: [ "-conf", "/etc/coredns/Corefile" ]
        volumeMounts:
        - name: config-volume
          mountPath: /etc/coredns
          readOnly: true
        - name: tmp
          mountPath: /tmp
        ports:
        - containerPort: 53
          name: dns
          protocol: UDP
        - containerPort: 53
          name: dns-tcp
          protocol: TCP
        - containerPort: 9153
          name: metrics
          protocol: TCP
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            add:
            - NET_BIND_SERVICE
            drop:
            - all
          readOnlyRootFilesystem: true
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
            scheme: HTTP
          initialDelaySeconds: 60
          timeoutSeconds: 5
          successThreshold: 1
          failureThreshold: 5
        readinessProbe:
          httpGet:
            path: /health
            port: 8080
            scheme: HTTP
      dnsPolicy: Default
      volumes:
        - name: tmp
          emptyDir: {}
        - name: config-volume
          configMap:
            name: coredns
            items:
            - key: Corefile
              path: Corefile
---
apiVersion: v1
kind: Service
metadata:
  name: kube-dns
  namespace: kube-system
  annotations:
    prometheus.io/port: "9153"
    prometheus.io/scrape: "true"
  labels:
    k8s-app: kube-dns
    kubernetes.io/cluster-service: "true"
    kubernetes.io/name: "CoreDNS"
spec:
  selector:
    k8s-app: kube-dns
  clusterIP: 10.32.0.10
  ports:
  - name: dns
    port: 53
    protocol: UDP
  - name: dns-tcp
    port: 53
    protocol: TCP
  - name: metrics
    port: 9153
    protocol: TCP`)
}
