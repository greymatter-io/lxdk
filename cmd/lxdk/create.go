package main

import (
	"fmt"
	"os"
	"path"

	certs "github.com/greymatter-io/lxdk/certificates"
	"github.com/greymatter-io/lxdk/config"
	"github.com/greymatter-io/lxdk/lxd"
	lxdclient "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

var (
	createCmd = &cli.Command{
		Name:  "create",
		Usage: "create a cluster",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "storage-driver",
				Usage: "lxd storage pool driver to use",
				Value: "btrfs",
			},
			&cli.StringFlag{
				Name:  "storage-pool",
				Usage: "lxd storage pool use, overrides storage pool creation",
			},
			&cli.StringFlag{
				Name:  "network",
				Usage: "network id of lxd network to use, overrides network creation",
			},
		},
		Action: doCreate,
	}
)

func doCreate(ctx *cli.Context) error {
	var state config.ClusterState
	state.StorageDriver = ctx.String("storage-driver")
	state.StoragePool = ctx.String("storage_pool")
	state.NetworkID = ctx.String("network")

	cacheDir := ctx.String("cache")

	if ctx.Args().Len() == 0 {
		return errors.New("must supply cluster name")
	}
	clusterName := ctx.Args().First()
	state.Name = clusterName

	path := path.Join(cacheDir, clusterName)

	_, err := os.Stat(path)
	if err == nil {
		return errors.Errorf("cluster %s already exists at path %s", clusterName, path)
	}

	if state.NetworkID == "" {
		networkID, err := createNetwork(state)
		if err != nil {
			return err
		}
		state.NetworkID = networkID
	}

	if state.StoragePool == "" {
		state.StoragePool, err = createStoragePool(state)
		if err != nil {
			return err
		}
	}

	containers, err := createContainers(state)
	if err != nil {
		return err
	}
	state.Containers = append(state.Containers, containers...)

	err = createCerts(path)
	if err != nil {
		return err
	}

	err = config.WriteClusterState(ctx, state)
	if err != nil {
		return fmt.Errorf("error reading cluster config: %w", err)
	}

	return nil
}

func createNetwork(state config.ClusterState) (string, error) {
	networkID := "lxdk-" + state.Name

	is, err := lxd.InstanceServerConnect()
	if err != nil {
		return "", err
	}

	networkPost := api.NetworksPost{}
	networkPost.Name = networkID
	networkPost.Config = map[string]string{"ipv6.address": "none"}
	is.CreateNetwork(networkPost)

	return networkID, err
}

func createCerts(cacheDir string) error {
	path := path.Join(cacheDir, "certificates")
	err := os.MkdirAll(path, 0777)
	if err != nil {
		return fmt.Errorf("error creating certificates dir: %w", err)
	}

	// k8s CA
	kubeCAConf := certs.CAConfig{
		Name: "ca",
		Dir:  path,
		CN:   "Kubernetes",
	}
	err = certs.CreateCA(kubeCAConf)
	if err != nil {
		return err
	}

	// aggregate CA
	aggregateCAConf := certs.CAConfig{
		Name: "ca-aggregation",
		Dir:  path,
		CN:   "Kubernetes Front Proxy CA",
	}
	err = certs.CreateCA(aggregateCAConf)
	if err != nil {
		return err
	}

	// etcd CA
	etcdCAConf := certs.CAConfig{
		Name: "ca-etcd",
		Dir:  path,
		CN:   "etcd",
	}
	err = certs.CreateCA(etcdCAConf)
	if err != nil {
		return err
	}

	// CA config
	caConfigPath, err := certs.WriteCAConfig(path)
	if err != nil {
		return err
	}

	// admin cert
	adminCertConfig := certs.CertConfig{
		Name:         "admin",
		CN:           "system:masters",
		CA:           kubeCAConf,
		Dir:          path,
		CAConfigPath: caConfigPath,
	}
	err = certs.CreateCert(adminCertConfig)
	if err != nil {
		return err
	}

	// aggregation client cert
	aggCertConfig := certs.CertConfig{
		Name:         "aggregation-client",
		JSONOverride: certs.CertJSON("kube-apiserver", "kube-apiserver"),
		CA:           aggregateCAConf,
		Dir:          path,
		CAConfigPath: caConfigPath,
	}
	err = certs.CreateCert(aggCertConfig)
	if err != nil {
		return err
	}

	// kube-controler-manager
	kubeConManConfig := certs.CertConfig{
		Name:         "kube-controller-manager",
		CN:           "kube-controller-manager",
		CA:           kubeCAConf,
		Dir:          path,
		CAConfigPath: caConfigPath,
	}
	err = certs.CreateCert(kubeConManConfig)
	if err != nil {
		return err
	}

	// kube-scheduler
	kubeSchedCertConfig := certs.CertConfig{
		Name:         "system:kube-scheduler",
		FileName:     "kube-scheduler",
		CN:           "system:kube-scheduler",
		CA:           kubeCAConf,
		Dir:          path,
		CAConfigPath: caConfigPath,
	}
	err = certs.CreateCert(kubeSchedCertConfig)
	if err != nil {
		return err
	}

	return nil
}

func createStoragePool(state config.ClusterState) (string, error) {
	is, err := lxd.InstanceServerConnect()
	if err != nil {
		return "", err
	}

	stPoolPost := api.StoragePoolsPost{
		Name:   "lxdk-" + state.Name,
		Driver: state.StorageDriver,
	}
	err = is.CreateStoragePool(stPoolPost)
	if err != nil {
		return "", err
	}

	return stPoolPost.Name, nil
}

func createContainers(state config.ClusterState) ([]string, error) {
	is, err := lxd.InstanceServerConnect()
	if err != nil {
		return nil, err
	}

	// TODO: modify this later so all workers have unique IDs
	containers := make([]string, 3)
	for i, image := range []string{"etcd", "controller", "worker"} {
		containerName, err := createContainer(image, is, state)

		if err != nil {
			return nil, err
		}

		containers[i] = containerName
	}

	return containers, err
}

// TODO: will have to put this somewhere else later to create workers
func createContainer(imageName string, is lxdclient.InstanceServer, state config.ClusterState) (string, error) {
	// TODO: kubdee applies a default profile to everything

	conf := api.InstancesPost{
		Name: fmt.Sprintf("lxdk-%s-%s", state.Name, imageName),
		Source: api.InstanceSource{
			Type:  "image",
			Alias: "kubedee-" + imageName,
		},
		Type: "container",
	}
	conf.Devices = map[string]map[string]string{
		"root": {
			"type": "disk",
			"pool": state.StoragePool,
			"path": "/",
		},
	}

	// add network to container
	net, _, err := is.GetNetwork(state.NetworkID)
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
