package containers

import (
	"fmt"
	"log"
	"math/rand"
	"strings"

	lxd "github.com/lxc/lxd/client"
	lxdclient "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
)

type ContainerConfig struct {
	ImageName   string
	ClusterName string
	StoragePool string
	NetworkID   string
}

func CreateContainer(config ContainerConfig, is lxdclient.InstanceServer) (string, error) {
	// TODO: kubdee applies a default profile to everything

	conf := api.InstancesPost{
		Name: fmt.Sprintf("lxdk-%s-%s-%s", config.ClusterName, config.ImageName, createID()),
		Source: api.InstanceSource{
			Type:  "image",
			Alias: "kubedee-" + config.ImageName,
		},
		Type: "container",
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
		return "", err
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

func DeleteContainer(name string, is lxd.InstanceServer) error {
	reqState := api.InstanceStatePut{
		Action:  "stop",
		Timeout: -1,
	}
	op, err := is.UpdateInstanceState(name, reqState, "")
	if err != nil {
		return err
	}

	err = op.Wait()
	if err != nil {
		log.Println("instance is already stopped, continuing")
	}

	op, err = is.DeleteInstance(name)
	if err != nil {
		return err
	}

	if err := op.Wait(); err != nil {
	}

	return nil
}

func GetContainerIP(name string, is lxd.InstanceServer) (string, error) {
	in, _, err := is.GetInstanceFull(name)
	if err != nil {
		return "", fmt.Errorf("error getting instance: %w", err)
	}

	var ips []string
	for _, net := range in.State.Network {
		if net.Type == "loopback" {
			continue
		}

		for _, addr := range net.Addresses {
			if addr.Scope == "link" || addr.Scope == "local" {
				continue
			}

			if strings.Contains(addr.Family, "inet") {
				ips = append(ips, addr.Address)
			}
		}
	}

	if len(ips) == 0 {
		return "", fmt.Errorf("container %s has no IP address", name)
	}

	return ips[0], nil
}

func createID() string {
	validChars := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ123456789")

	b := make([]rune, 5)
	for i := range b {
		b[i] = validChars[rand.Intn(len(validChars))]
	}

	return string(b)
}
