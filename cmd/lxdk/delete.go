package main

import (
	"os"
	"path"

	"github.com/greymatter-io/lxdk/config"
	"github.com/greymatter-io/lxdk/lxd"
	"github.com/lxc/lxd/shared/api"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

var deleteCmd = &cli.Command{
	Name:   "delete",
	Usage:  "delete a cluster",
	Action: doDelete,
}

func doDelete(ctx *cli.Context) error {
	cacheDir := ctx.String("cache")

	clusterName := ctx.Args().First()
	if clusterName == "" {
		return errors.New("must supply cluster name")
	}
	path := path.Join(cacheDir, clusterName)

	err := deleteContainersFromContext(ctx)
	if err != nil {
		return err
	}

	err = deleteStoragePoolFromContext(ctx)
	if err != nil {
		return err
	}

	err = deleteNetworkFromContext(ctx)
	if err != nil {
		return err
	}

	err = os.RemoveAll(path)
	if err != nil {
		return err
	}

	return nil
}

func deleteNetworkFromContext(ctx *cli.Context) error {
	clusterConfig, err := config.ClusterConfigFromContext(ctx)
	if err != nil {
		return err
	}

	is, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}
	err = is.DeleteNetwork(clusterConfig.NetworkID)
	if err != nil {
		return err
	}

	return nil
}

func deleteStoragePoolFromContext(ctx *cli.Context) error {
	clusterConfig, err := config.ClusterConfigFromContext(ctx)
	if err != nil {
		return err
	}

	is, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}
	err = is.DeleteStoragePool(clusterConfig.Name)
	if err != nil {
		return err
	}

	return nil
}

func deleteContainersFromContext(ctx *cli.Context) error {
	clusterConfig, err := config.ClusterConfigFromContext(ctx)
	if err != nil {
		return err
	}

	is, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

	reqState := api.InstanceStatePut{
		Action:  "stop",
		Timeout: -1,
	}
	for _, name := range clusterConfig.Containers {
		op, err := is.UpdateInstanceState(name, reqState, "")
		if err != nil {
			return err
		}

		err = op.Wait()
		if err != nil {
			return err
		}

		op, err = is.DeleteInstance(name)
		if err != nil {
			return err
		}

		if err := op.Wait(); err != nil {
		}
	}

	return nil
}

//kubedee [options] delete <cluster name>            delete a cluster
