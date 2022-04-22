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
	state, err := config.ClusterStateFromContext(ctx)
	if err != nil {
		return err
	}

  // TODO(cm): do we actually check with the real api?
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
	if err := config.WriteClusterState(ctx, state); err != nil {
		return err
	}

	return nil
}
