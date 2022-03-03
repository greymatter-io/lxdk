package config

import (
	"github.com/BurntSushi/toml"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

type Config struct {
	ControllerLimitsCPU    int    `toml:"controller_limits_cpu"`
	ControllerLimitsMemory string `toml:"controller_limits_memory"`
	WorkerLimitsCPU        int    `toml:"worker_limits_cpu"`
	WorkerLimitsMemory     string `toml:"worker_limits_memory"`
	StoragePool            string `toml:"storage_pool"`
	RootFSSize             string `toml:"root_fs_size"`
	EnableInsecureRegistry bool   `toml:"enable_insecure_registry"`
}

func CLIConfigFromCLIContext(context *cli.Context) (Config, error) {
	var conf Config
	_, err := toml.DecodeFile(context.String("config"), &conf)
	if err != nil {
		return conf, errors.Wrap(err, "error loading config file")
	}

	return conf, nil
}

//--apiserver-extra-hostnames <hostname>[,<hostname>]   additional X509v3 Subject Alternative Name to set, comma separated
//--bin-dir <dir>                                       where to copy the k8s binaries from (default: ./_output/bin)
//--kubernetes-version <version>                        the release of Kubernetes to install, for example 'v1.12.0'
//takes precedence over `--bin-dir`
//--no-set-context                                      prevent kubedee from adding a new kubeconfig context
//--num-worker <num>                                    number of worker nodes to start (default: 2)
//--use-host-binaries                                   allow using binaries from the host within cluster containers
//--vm                                                  launch LXD virtual machines instead of containers
//--controller-limits-cpu <num>                         set controller container/VM `limits.cpu` (default: 12)
//--controller-limits-memory <amount>                   set controller container/VM `limits.memory` (default: 4GiB)
//--worker-limits-cpu <num>                             set worker container/VM `limits.cpu` (default: 12)
//--worker-limits-memory <amount>                       set worker container/VM `limits.memory` (default: 4GiB)
//--storage-pool <pool_name>                            set LXD storage pool (default: kubedee)
//--rootfs-size <size>                                  set LXD VM rootfs volume size (default: 20GiB)
//--enable-insecure-registry                            launch insecure OCI image registry in cluster network
