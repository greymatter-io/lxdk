package main

import (
	"fmt"
	"path"

	"github.com/greymatter-io/lxdk/config"
	"github.com/greymatter-io/lxdk/containers"
	"github.com/greymatter-io/lxdk/lxd"
	"github.com/urfave/cli/v2"
)

var startworkerCmd = &cli.Command{
	Name:   "start-worker",
	Usage:  "start a new worker node in a cluster",
	Action: doStartWorker,
}

func doStartWorker(ctx *cli.Context) error {
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

	if state.RunState != config.Running {
		return fmt.Errorf("cluster %s is not running or was not started by lxdk", state.Name)
	}

	is, _, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

	conf := containers.ContainerConfig{
		ImageName:   "worker",
		ClusterName: state.Name,
		StoragePool: state.StoragePool,
		NetworkID:   state.NetworkID,
	}

	containerName, err := containers.CreateContainer(conf, is)
	if err != nil {
		return err
	}

	if err := containers.StartContainer(containerName, is); err != nil {
		return err
	}

	if err = createWorkerCert(containerName, certDir, is); err != nil {
		return err
	}

	registryIP, err := containers.WaitContainerIP(state.RegistryContainerName, is)
	if err != nil {
		return err
	}

	controllerIP, err := containers.WaitContainerIP(state.ControllerContainerName, is)
	if err != nil {
		return err
	}

	etcdIP, err := containers.WaitContainerIP(state.EtcdContainerName, is)
	if err != nil {
		return err
	}

	containerConfig := workerConfig{
		ContainerName: containerName,
		ControllerIP:  controllerIP.String(),
		RegistryName:  state.RegistryContainerName,
		RegistryIP:    registryIP.String(),
		EtcdIP:        etcdIP.String(),
		ClusterDir:    path.Join(cacheDir, state.Name),
	}
	err = configureWorker(containerConfig, is)
	if err != nil {
		return err
	}

	state.Containers = append(state.Containers, containerName)
	state.WorkerContainerNames = append(state.WorkerContainerNames, containerName)
	if err := config.WriteClusterState(ctx, state); err != nil {
		return err
	}

	return nil
}
