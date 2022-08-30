package containers

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"os"
	"path"
	"strings"
	"syscall"
	"time"

	lxd "github.com/lxc/lxd/client"
	lxdclient "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
)

var (
	UID int64
	GID int64
)

type ContainerConfig struct {
	ImageName   string
	ClusterName string
	StoragePool string
	NetworkID   string
}

// CreateContainerProfile sets up a container profile suitable for running Kubernetes
// workloads.
func CreateContainerProfile(is lxdclient.InstanceServer) error {
	prof, _, err := is.GetProfile("default")
	if err != nil {
		return fmt.Errorf("could not get lxd default profile: %w", err)
	}

	newProf := api.ProfilesPost{
		Name: "lxdk",
	}
	newProf.Devices = prof.Devices
	newProf.Config = map[string]string{
		"raw.lxc": `lxc.apparmor.profile=unconfined
lxc.mount.auto=proc:rw sys:rw cgroup:rw
lxc.init.cmd=/sbin/init systemd.unified_cgroup_hierarchy=0
lxc.cgroup.devices.allow=a
lxc.cgroup2.devices.allow=a
lxc.cap.drop=
lxc.apparmor.allow_incomplete=1`,
		"security.privileged":  "true",
		"security.nesting":     "true",
		"linux.kernel_modules": "ip_tables,ip6_tables,netlink_diag,nf_nat,overlay",
	}

	err = is.CreateProfile(newProf)
	if err != nil {
		return fmt.Errorf("could not create profile: %w", err)
	}

	return nil
}

func CreateContainer(config ContainerConfig, is lxdclient.InstanceServer) (string, error) {
	// TODO: https://github.com/zer0def/kubedee/blob/master/lib.bash#L1116
	// kubdee applies unified_profile if cgroup=2

	conf := api.InstancesPost{
		Name: fmt.Sprintf("lxdk-%s-%s-%s", config.ClusterName, config.ImageName, createID()),
		Source: api.InstanceSource{
			Type:  "image",
			Alias: "kubedee-" + config.ImageName,
		},
		Type: "container",
	}

	if !strings.Contains(conf.Name, "etcd") {
		conf.Profiles = []string{"lxdk"}
	} else {
		conf.Config = map[string]string{"raw.lxc": "lxc.apparmor.allow_incomplete=1"}
	}

	if config.ImageName == "registry" {
		conf.Source.Alias = "kubedee-worker"
	}

	conf.Devices = map[string]map[string]string{
		"root": {
			"type": "disk",
			"pool": config.StoragePool,
			"path": "/",
		},
	}

	// add network to container
	net, _, err := is.GetNetwork(config.NetworkID)
	if err != nil {
		return "", err
	}

	var device map[string]string
	if net.Managed && is.HasExtension("instance_nic_network") {
		device = map[string]string{
			"type":    "nic",
			"network": net.Name,
		}
	} else {
		device = map[string]string{
			"type":    "nic",
			"nictype": "macvlan",
			"parent":  net.Name,
		}

		if net.Type == "bridge" {
			device["nictype"] = "bridged"
		}
	}
	device["name"] = "eth0"

	conf.Devices["eth0"] = device

	op, err := is.CreateInstance(conf)
	if err != nil {
		return "", fmt.Errorf("there was an error creating the instance: (%w), does the image '%s' exist?", err, "kuedee-"+config.ImageName)
	}

	err = op.Wait()
	if err != nil {
		return "", err
	}

	return conf.Name, nil
}

func StartContainer(containerName string, is lxd.InstanceServer) error {
	reqState := api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}

	op, err := is.UpdateInstanceState(containerName, reqState, "")
	if err != nil {
		return err
	}

	err = op.Wait()
	if err != nil {
		return err
	}

	return nil
}

func StopContainer(containerName string, is lxd.InstanceServer) error {
	reqState := api.InstanceStatePut{
		Action:  "stop",
		Timeout: -1,
	}
	op, err := is.UpdateInstanceState(containerName, reqState, "")
	if err != nil {
		return err
	}

	err = op.Wait()
	if err != nil {
		log.Println("instance is already stopped, continuing")
	}

	return nil
}

func DeleteContainer(containerName string, is lxd.InstanceServer) error {
	if err := StopContainer(containerName, is); err != nil {
		return err
	}

	op, err := is.DeleteInstance(containerName)
	if err != nil {
		return err
	}

	if err := op.Wait(); err != nil {
	}

	return nil
}

func WaitContainerIP(name string, blacklist []string, is lxd.InstanceServer) (net.IP, error) {
	var ip net.IP
	var err error
	ip, err = GetContainerLXDIP(name, blacklist, is)
	for c := 0; c < 50 && err != nil; c++ {
		log.Default().Printf("waiting for %s to get an IP address...", name)
		time.Sleep(2 * time.Second)
		ip, err = GetContainerLXDIP(name, blacklist, is)
	}
	if err != nil {
		return nil, err
	}
	return ip, nil
}

// GetContainerLXDIP returns the IP address of the container on the LXD network
// (and not the cni0 flannel.1 network devices). This is usually on eth0.
func GetContainerLXDIP(name string, blacklist []string, is lxd.InstanceServer) (net.IP, error) {
	in, _, err := is.GetInstanceFull(name)
	if err != nil {
		return nil, fmt.Errorf("error getting instance: %w", err)
	}

	var ips []string
	for netName, net := range in.State.Network {
		if net.Type == "loopback" {
			continue
		}

		for _, addr := range net.Addresses {
			if addr.Scope == "link" || addr.Scope == "local" {
				continue
			}

			if strings.Contains(addr.Family, "inet") && netName != "cni0" && netName != "flannel.1" {
				blacklisted := false
				for _, black := range blacklist {
					if strings.Contains(addr.Address, black) {
						blacklisted = true
						break
					}
				}
				if strings.Count(addr.Address, ":") < 2 && !blacklisted {
					ips = append(ips, addr.Address)
				}
			}
		}
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("container %s has no IP address", name)
	}

	ip := net.ParseIP(ips[0])
	if ip == nil {
		return nil, fmt.Errorf("not a valid ip: %s", ips[0])
	}

	return ip, nil
}

// from is a file, to is a dir
func UploadFile(data []byte, from, to, container string, is lxdclient.InstanceServer) error {
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

		err = RecursiveMkdir(container, to, mode, UID, GID, is)
		if err != nil {
			return err
		}
	} else {
		// if data exists, to should be a filename and we have to
		// let lxc infer the UID and GID
		toPath = to
		mode = os.FileMode(0755)

		toDir, _ := path.Split(to)
		err := RecursiveMkdir(container, toDir, mode, UID, GID, is)
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

func RecursiveMkdir(container, dir string, mode os.FileMode, UID, GID int64, is lxdclient.InstanceServer) error {
	if dir == "/" {
		return nil
	}

	if strings.HasSuffix(dir, "/") {
		dir = dir[:len(dir)-1]
	}

	split := strings.Split(dir[1:], "/")
	if len(split) > 1 {
		err := RecursiveMkdir(container, "/"+strings.Join(split[:len(split)-1], "/"), mode, UID, GID, is)
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

func UploadFiles(froms []string, to, container string, is lxdclient.InstanceServer) error {
	for _, from := range froms {
		err := UploadFile(nil, from, to, container, is)
		if err != nil {
			return err
		}
	}
	return nil
}

func RunCommand(container, command string, is lxdclient.InstanceServer) error {
	split := strings.Fields(command)

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

func RunCommands(container string, commands []string, is lxdclient.InstanceServer) error {
	for _, command := range commands {
		err := RunCommand(container, command, is)
		if err != nil {
			return err
		}
	}

	return nil
}

func createID() string {
	validChars := []rune("abcdefghijklmnopqrstuvwxyz0123456789")

	b := make([]rune, 5)
	for i := range b {
		b[i] = validChars[rand.Intn(len(validChars))]
	}

	return string(b)
}
