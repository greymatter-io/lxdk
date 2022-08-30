package main

import "github.com/urfave/cli/v2"

import "github.com/hashicorp/go-getter/v2"

var downloadCmd = &cli.Command{
	Name:                   "download",
	Aliases:                nil,
	Usage:                  "download Kubernetes binaries",
	UsageText:              "",
	Description:            "",
	ArgsUsage:              "",
	Category:               "",
	BashComplete:           nil,
	Before:                 nil,
	After:                  nil,
	Action:                 doDownload,
	OnUsageError:           nil,
	Subcommands:            nil,
	Flags:                  nil,
	SkipFlagParsing:        false,
	HideHelp:               false,
	HideHelpCommand:        false,
	Hidden:                 false,
	UseShortOptionHandling: false,
	HelpName:               "",
	CustomHelpTemplate:     "",
}

func doDownload(clictx *cli.Context) error {
	_ = getter.Getters[0]
	return nil
}
