package main

import (
	"fmt"

	"github.com/greymatter-io/lxdk/config"
	"github.com/urfave/cli/v2"
)

var listCmd = &cli.Command{
	Name:    "list",
	Usage:   "list clusters",
	Aliases: []string{"ls"},
	Action:  doList,
}

func doList(ctx *cli.Context) error {
	stateManager, err := config.ClusterStateManagerFromContext(ctx)
	if err != nil {
		return err
	}

	clusters, err := stateManager.List()
	if err != nil {
		return err
	}

	for _, cluster := range clusters {
		fmt.Println(cluster)
	}

	return nil
}
