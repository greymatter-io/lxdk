package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"strings"

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
			&cli.IntFlag{
				Name:  "num-workers",
				Usage: "the number of worker nodes to create",
				Value: 1,
			},
		},
		Action: doCreate,
	}
)

func doCreate(ctx *cli.Context) error {

	// init our self-managed state
	var state config.ClusterState
	state.StorageDriver = ctx.String("storage-driver")
	state.StoragePool = ctx.String("storage-pool")
	state.NetworkID = ctx.String("network")

	state.RunState = config.Uninitialized

	// connect to LXD
	is, _, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

	cacheDir := ctx.String("cache")

	if ctx.Args().Len() == 0 {
		return errors.New("must supply cluster name")
	}
	clusterName := ctx.Args().First()
	state.Name = clusterName

	cachePath := path.Join(cacheDir, clusterName)

	// if the folder exists, we consider the cluster "created"
	_, err = os.Stat(cachePath)
	if err == nil {
		return errors.Errorf("cluster %s already exists at path %s", clusterName, cachePath)
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
			// delete network if create storage pool fails?
			if err := deleteNetwork(state, is); err != nil {
				log.Default().Printf("network %s was not deleted", state.NetworkID)
			}

			return err
		}
	}
	log.Default().Println("using storage pool:", state.StoragePool)

	if err := ensureLXDKProfileExists(is); err != nil {
		return err
	}

	containerNames, err := createContainers(state, ctx.Int("num-workers"), is)
	if err != nil {
		cleanupStateOnErr(is, state)
		return err
	}

	state.EtcdContainerName = containerNames["etcd"][0]
	state.ControllerContainerName = containerNames["controller"][0]
	state.RegistryContainerName = containerNames["registry"][0]
	state.WorkerContainerNames = containerNames["worker"]

	for _, names := range containerNames {
		for _, name := range names {
			state.Containers = append(state.Containers, name)
		}
	}

	err = config.WriteClusterState(ctx, state)
	if err != nil {
		return fmt.Errorf("error reading cluster config: %w", err)
	}

	err = createCerts(cachePath)
	if err != nil {
		return err
	}

	return nil
}

// delete our dedicated network and storage pool
func cleanupStateOnErr(is lxdclient.InstanceServer, state config.ClusterState) {
	if err := deleteNetwork(state, is); err != nil {
		log.Default().Printf("network %s was not deleted", state.NetworkID)
	}
	// avoid deleting "default" storage pool
	if strings.Contains(state.StoragePool, "lxdk") {
		if err := deleteStoragePool(state, is); err != nil {
			log.Default().Printf("storage pool %s was not deleted", state.StoragePool)
		}
	}
}

func ensureLXDKProfileExists(is lxdclient.InstanceServer) error {
	profs, err := is.GetProfileNames()
	if err != nil {
		return err
	}

	for _, prof := range profs {
		if prof == "lxdk" {
			// Profile exists. Return.
			return nil
		}
	}
	// We didn't find the lxdk profile. Create it.
	return containers.CreateContainerProfile(is)
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
		CN:           "system:kube-controller-manager",
		CA:           kubeCAConf,
		Dir:          path,
		CAConfigPath: caConfigPath,
		JSONOverride: certs.CertJSON("system:kube-controller-manager", "system:kube-controller-manager"),
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
	if err := is.CreateStoragePool(stPoolPost); err != nil {
		return "", err
	}

	return stPoolPost.Name, nil
}

func createContainers(state config.ClusterState, numWorkers int, is lxdclient.InstanceServer) (map[string][]string, error) {
	created := make(map[string][]string)
	created["worker"] = []string{}
	for _, image := range []string{"etcd", "controller", "worker", "registry"} {
		for i := 0; i < numWorkers; i++ {
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

			if image != "worker" {
				created[image] = []string{containerName}
				break
			}

			created["worker"] = append(created["worker"], containerName)
		}
	}

	return created, nil
}
