package main

import "github.com/urfave/cli/v2"

var upCmd = &cli.Command{
	Name:   "up",
	Usage:  "create + start in one command",
	Flags:  createCmd.Flags,
	Action: doUp,
}

func doUp(ctx *cli.Context) error {
	err := doCreate(ctx)
	if err != nil {
		return err
	}

	err = doStart(ctx)
	if err != nil {
		return err
	}

	return nil
}
