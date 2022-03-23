package main

import (
	"fmt"
	"os"
	"path"

	certs "github.com/greymatter-io/lxdk/certificates"
	"github.com/greymatter-io/lxdk/config"
	"github.com/greymatter-io/lxdk/containers"
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
	state.StoragePool = ctx.String("storage-pool")
	state.NetworkID = ctx.String("network")

	is, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

	cacheDir := ctx.String("cache")

	if ctx.Args().Len() == 0 {
		return errors.New("must supply cluster name")
	}
	clusterName := ctx.Args().First()
	state.Name = clusterName

	path := path.Join(cacheDir, clusterName)

	_, err = os.Stat(path)
	if err == nil {
		return errors.Errorf("cluster %s already exists at path %s", clusterName, path)
	}

	if state.NetworkID == "" {
		networkID, err := createNetwork(state, is)
		if err != nil {
			return err
		}
		state.NetworkID = networkID
	}

	if state.StoragePool == "" {
		state.StoragePool, err = createStoragePool(state, is)
		if err != nil {
			return err
		}
	}

	err = containers.CreateContainerProfile(is)
	if err != nil {
		return err
	}

	containerNames, err := createContainers(state, is)
	if err != nil {
		return err
	}
	state.EtcdContainerName = containerNames["etcd"]
	state.ControllerContainerName = containerNames["controller"]
	state.RegistryContainerName = containerNames["registry"]
	state.WorkerContainerNames = []string{containerNames["worker"]}

	for _, v := range containerNames {
		state.Containers = append(state.Containers, v)
	}

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

func createNetwork(state config.ClusterState, is lxdclient.InstanceServer) (string, error) {
	networkID := "lxdk-" + state.Name

	networkPost := api.NetworksPost{}
	networkPost.Name = networkID
	networkPost.Config = map[string]string{"ipv6.address": "none"}
	err := is.CreateNetwork(networkPost)
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
		CN:           "admin",
		JSONOverride: certs.CertJSON("admin", "system:masters"),
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

	// kube-proxy
	kubeProxyCertConfig := certs.CertConfig{
		Name:         "system:node-proxier",
		FileName:     "kube-proxy",
		CN:           "system:kube-proxy",
		CA:           kubeCAConf,
		Dir:          path,
		CAConfigPath: caConfigPath,
	}
	err = certs.CreateCert(kubeProxyCertConfig)
	if err != nil {
		return err
	}

	return nil
}

func createStoragePool(state config.ClusterState, is lxdclient.InstanceServer) (string, error) {
	stPoolPost := api.StoragePoolsPost{
		Name:   "lxdk-" + state.Name,
		Driver: state.StorageDriver,
	}
	err := is.CreateStoragePool(stPoolPost)
	if err != nil {
		return "", err
	}

	return stPoolPost.Name, nil
}

func createContainers(state config.ClusterState, is lxdclient.InstanceServer) (map[string]string, error) {
	created := make(map[string]string)
	for _, image := range []string{"etcd", "controller", "worker", "registry"} {
		conf := containers.ContainerConfig{
			ImageName:   image,
			ClusterName: state.Name,
			StoragePool: state.StoragePool,
			NetworkID:   state.NetworkID,
		}
		containerName, err := containers.CreateContainer(conf, is)

		if err != nil {
			return nil, err
		}

		created[image] = containerName
	}

	return created, nil
}
