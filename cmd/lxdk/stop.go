package main

import (
	"fmt"

	"github.com/greymatter-io/lxdk/config"
	"github.com/greymatter-io/lxdk/containers"
	"github.com/greymatter-io/lxdk/lxd"
	"github.com/urfave/cli/v2"
)

var (
	stopCmd = &cli.Command{
		Name:   "stop",
		Usage:  "stop a cluster",
		Action: doStop,
	}
)

func doStop(ctx *cli.Context) error {
	stateManager, err := config.ClusterStateManagerFromContext(ctx)
	if err != nil {
		return err
	}

	if ctx.Args().Len() == 0 {
		return fmt.Errorf("must supply cluster name")
	}
	clusterName := ctx.Args().First()

	state, err := stateManager.Pull(clusterName)
	if err != nil {
		return err
	}

	if state.RunState == config.Stopped {
		return fmt.Errorf("cluster %s is already stopped or was not started by lxdk", state.Name)
	}

	is, _, err := lxd.InstanceServerConnect()
	if err != nil {
		return err
	}

	for _, container := range state.Containers {
		if err := containers.StopContainer(container, is); err != nil {
			return err
		}
	}

	state.RunState = config.Stopped
	if err := stateManager.Cache(state); err != nil {
		return err
	}

	err = stateManager.Push(state)
	if err != nil {
		return err
	}

	return nil
}
