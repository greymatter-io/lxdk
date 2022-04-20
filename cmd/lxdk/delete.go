package main

import (
	"log"
	"os"
	"path"
	"strings"

	"github.com/greymatter-io/lxdk/config"
	"github.com/greymatter-io/lxdk/containers"
	"github.com/greymatter-io/lxdk/lxd"
	lxdclient "github.com/lxc/lxd/client"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

var deleteCmd = &cli.Command{
	Name:  "delete",
	Usage: "delete a cluster",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "delete-storage",
			Usage: "whether or not to delete the associated storage pool",
			Value: true,
		},
		&cli.BoolFlag{
			Name:  "delete-network",
			Usage: "whether or not to delete the associated network",
			Value: true,
		},
	},
	Action: doDelete,
}

func doDelete(ctx *cli.Context) error {
	cacheDir := ctx.String("cache")

	clusterName := ctx.Args().First()
	if clusterName == "" {
		return errors.New("must supply cluster name")
	}
	path := path.Join(cacheDir, clusterName)

	state, err := config.ClusterStateFromContext(ctx)
	if err != nil {
		return err
	}

	is, _, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

	err = deleteContainers(state, is)
	if err != nil {
		return err
	}

	if !strings.Contains(state.StoragePool, "lxdk") {
		log.Default().Printf("storage pool %s was not created by lxdk and will not be deleted", state.StoragePool)
	}
	if ctx.Bool("delete-storage") && strings.Contains(state.StoragePool, "lxdk") {
		err = deleteStoragePool(state, is)
		if err != nil {
			return err
		}
	}

	if ctx.Bool("delete-network") && strings.Contains(state.NetworkID, "lxdk") {
		err = deleteNetwork(state, is)
		if err != nil {
			return err
		}
	}

	err = os.RemoveAll(path)
	if err != nil {
		return err
	}

	return nil
}

func deleteNetwork(state config.ClusterState, is lxdclient.InstanceServer) error {
	err := is.DeleteNetwork(state.NetworkID)
	if err != nil {
		return err
	}

	return nil
}

func deleteStoragePool(state config.ClusterState, is lxdclient.InstanceServer) error {
	err := is.DeleteStoragePool(state.StoragePool)
	if err != nil {
		return err
	}

	return nil
}

func deleteContainers(state config.ClusterState, is lxdclient.InstanceServer) error {
	for _, name := range state.Containers {
		err := containers.DeleteContainer(name, is)
		if err != nil {
			return err
		}
	}

	return nil
}

func deleteProfile(state config.ClusterState, is lxdclient.InstanceServer) error {
	err := is.DeleteProfile("lxdk")
	if err != nil {
		return err
	}

	return nil
}
