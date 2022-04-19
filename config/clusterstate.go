package config

import (
	"os"
	"path"

	"github.com/BurntSushi/toml"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

type RunState int

const (
	Uninitialized RunState = iota
	Running
	Stopped
)

type ClusterState struct {
	Name           string `toml:"name"`
	NetworkID      string `toml:"network_id"`
	NetworkManaged bool   `toml:"network_managed"`

	Containers []string `toml:"containers"`

	RunState RunState `toml:"run_state"`

	EtcdContainerName       string   `toml:"etcd_container_name"`
	ControllerContainerName string   `toml:"controller_container_name"`
	RegistryContainerName   string   `toml:"registry_container_name"`
	WorkerContainerNames    []string `toml:"worker_container_names"`

	StorageDriver      string `toml:"storage_driver"`
	StoragePool        string `toml:"storage_pool"`
	StoragePoolManaged bool   `toml:"storage_pool_managed"`
}

func ClusterStateFromContext(ctx *cli.Context) (ClusterState, error) {
	var state ClusterState

	clusterName := ctx.Args().First()
	if clusterName == "" {
		return state, errors.New("must supply cluster name")
	}

	clusterConfigPath := path.Join(ctx.String("cache"), clusterName, "state.toml")
	_, err := toml.DecodeFile(clusterConfigPath, &state)
	if err != nil {
		return state, errors.Wrap(err, "error loading config file")
	}

	return state, nil
}

func WriteClusterState(ctx *cli.Context, state ClusterState) error {
	clusterName := ctx.Args().First()
	if clusterName == "" {
		return errors.New("must supply cluster name")
	}

	cacheDir := path.Join(ctx.String("cache"), clusterName)
	err := os.MkdirAll(cacheDir, 0777)
	if err != nil {
		return errors.Wrap(err, "error creating "+cacheDir)
	}

	clusterConfigPath := path.Join(cacheDir, "state.toml")
	w, err := os.Create(clusterConfigPath)
	if err != nil {
		return err
	}

	enc := toml.NewEncoder(w)
	err = enc.Encode(state)
	if err != nil {
		return err
	}

	return nil
}
