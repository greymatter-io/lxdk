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
	for _, container := range state.Containers {
		ip, err := containers.GetContainerIP(container, is)
		if err != nil {
			return err
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

		// TDOO: probably going to have to move this out
		// worker cert
		if strings.Contains(container, "worker") {
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

	// configure registry - would it be easier to clone the worker image and
	// copy in the docker registry and service config in the packer build?
	// https://github.com/zer0def/kubedee/blob/master/lib.bash#L795

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
	// create admin kubeconfig
	// configure rbac

	return nil
}

// from is a file, to is a dir
func uploadFile(from, to, container string, is lxdclient.InstanceServer) error {
	stat, err := os.Stat(from)
	if err != nil {
		return fmt.Errorf("cannot stat %s: %w", from, err)
	}

	var UID int64
	var GID int64
	if linuxstat, ok := stat.Sys().(*syscall.Stat_t); ok {
		UID = int64(linuxstat.Uid)
		GID = int64(linuxstat.Gid)
	} else {
		UID = int64(os.Getuid())
		GID = int64(os.Getgid())
	}
	mode := os.FileMode(0755)

	data, err := ioutil.ReadFile(from)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", from, err)
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

	err = recursiveMkDir(container, to, mode, UID, GID, is)
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
		err := uploadFile(from, to, container, is)
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
