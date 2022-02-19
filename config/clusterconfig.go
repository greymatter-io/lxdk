package config

import (
	"os"
	"path"

	"github.com/BurntSushi/toml"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

type ClusterConfig struct {
	Name       string   `toml:"name"`
	NetworkID  string   `toml:"network_id"`
	Containers []string `toml:"containers"`
}

func ClusterConfigFromContext(ctx *cli.Context) (ClusterConfig, error) {
	var conf ClusterConfig

	clusterName := ctx.Args().First()
	if clusterName == "" {
		return conf, errors.New("must supply cluster name")
	}

	clusterConfigPath := path.Join(ctx.String("cache"), clusterName, "config.toml")
	_, err := toml.DecodeFile(clusterConfigPath, &conf)
	if err != nil {
		return conf, errors.Wrap(err, "error loading config file")
	}

	return conf, nil
}

func WriteClusterConfig(ctx *cli.Context, conf ClusterConfig) error {
	clusterName := ctx.Args().First()
	if clusterName == "" {
		return errors.New("must supply cluster name")
	}

	cacheDir := path.Join(ctx.String("cache"), clusterName)
	err := os.MkdirAll(cacheDir, 0777)
	if err != nil {
		return errors.Wrap(err, "error creating "+cacheDir)
	}

	clusterConfigPath := path.Join(cacheDir, "config.toml")
	w, err := os.Create(clusterConfigPath)
	if err != nil {
		return err
	}

	enc := toml.NewEncoder(w)
	err = enc.Encode(conf)
	if err != nil {
		return err
	}

	return nil
}
