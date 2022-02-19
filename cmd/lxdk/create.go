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

//kubedee [options] create <cluster name>            create a cluster

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
		},
		Action: doCreate,
	}
)

func doCreate(ctx *cli.Context) error {
	clusterConfig := config.ClusterConfig{}

	cacheDir := ctx.String("cache")

	clusterName := ctx.Args().First()
	if clusterName == "" {
		return errors.New("must supply cluster name")
	}
	clusterConfig.Name = clusterName

	path := path.Join(cacheDir, clusterName)

	_, err := os.Stat(path)
	if err == nil {
		return errors.Errorf("cluster %s already exists at path %s", clusterName, path)
	}

	err = config.WriteClusterConfig(ctx, clusterConfig)
	if err != nil {
		return errors.Wrap(err, "error reading cluster config")
	}

	err = createNetworkFromContext(ctx)
	if err != nil {
		return err
	}

	err = createStoragePoolFromContext(ctx)
	if err != nil {
		return err
	}

	err = createContainersFromContext(ctx)
	if err != nil {
		return err
	}

	err = createCerts(path)
	if err != nil {
		return err
	}

	return nil
}

func createNetworkFromContext(ctx *cli.Context) error {
	clusterConfig, err := config.ClusterConfigFromContext(ctx)
	if err != nil {
		return err
	}

	networkID := "lxdk-" + clusterConfig.Name

	is, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

	networkPost := api.NetworksPost{}
	networkPost.Name = networkID
	networkPost.Config = map[string]string{"ipv6.address": "none"}
	is.CreateNetwork(networkPost)

	clusterConfig.NetworkID = networkID
	err = config.WriteClusterConfig(ctx, clusterConfig)
	if err != nil {
		return err
	}

	return err
}

func createCerts(cacheDir string) error {
	path := path.Join(cacheDir, "certificates")
	err := os.MkdirAll(path, 0777)
	if err != nil {
		return errors.Wrap(err, "error creating certificates dir")
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

	// TODO: etcd cert
	// TODO: controller cert
	// TODO: worker cert

	return nil
}

func createStoragePoolFromContext(ctx *cli.Context) error {
	is, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

	clusterConfig, err := config.ClusterConfigFromContext(ctx)
	if err != nil {
		return err
	}

	stPoolPost := api.StoragePoolsPost{
		Name:   clusterConfig.Name,
		Driver: ctx.String("storage-driver"),
	}
	err = is.CreateStoragePool(stPoolPost)
	if err != nil {
		return err
	}

	return nil
}

func createContainersFromContext(ctx *cli.Context) error {
	is, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

	clusterConfig, err := config.ClusterConfigFromContext(ctx)
	if err != nil {
		return err
	}

	// TODO: modify this later so all workers have unique IDs
	for _, image := range []string{"etcd", "controller", "worker"} {
		containerName := fmt.Sprintf("lxdk-%s-%s", clusterConfig.Name, image)
		err = createContainer(image, is, clusterConfig)
		if err != nil {
			return err
		}

		clusterConfig.Containers = append(clusterConfig.Containers, containerName)
	}

	err = config.WriteClusterConfig(ctx, clusterConfig)
	return err
}

// TODO: will have to put this somewhere else later to create workers
func createContainer(imageName string, is lxdclient.InstanceServer, clusterConfig config.ClusterConfig) error {
	// TODO: kubdee applies a default profile to everything

	conf := api.InstancesPost{
		Name: fmt.Sprintf("lxdk-%s-%s", clusterConfig.Name, imageName),
		Source: api.InstanceSource{
			Type:  "image",
			Alias: "kubedee-" + imageName,
		},
		Type: "container",
	}
	conf.Devices = map[string]map[string]string{
		"root": {
			"type": "disk",
			"pool": clusterConfig.Name,
			"path": "/",
		},
	}

	// add network to container
	net, _, err := is.GetNetwork(clusterConfig.NetworkID)
	if err != nil {
		return err
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
		return err
	}

	err = op.Wait()
	if err != nil {
		return err
	}

	return nil
}
