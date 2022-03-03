package main

import (
	"github.com/greymatter-io/lxdk/config"
	"github.com/greymatter-io/lxdk/lxd"
	"github.com/lxc/lxd/shared/api"
	"github.com/urfave/cli/v2"
)

var startCmd = &cli.Command{
	Name:   "start",
	Usage:  "start a cluster",
	Action: doStart,
}

// TODO: etcd cert
// TODO: controller cert
// TODO: worker cert

func doStart(ctx *cli.Context) error {
	state, err := config.ClusterStateFromContext(ctx)
	if err != nil {
		return err
	}

	for _, container := range state.Containers {
		err = startContainer(container)
		if err != nil {
			return err
		}
	}

	return nil
}

func startContainer(containerName string) error {
	is, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

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

//kubedee [options] start <cluster name>             start a cluster
