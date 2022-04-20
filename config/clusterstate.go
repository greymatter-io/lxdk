package config

type RunState int

const (
	Uninitialized RunState = iota
	Running
	Stopped
)

type ClusterState struct {
	Name      string `toml:"name"`
	NetworkID string `toml:"network_id"`

	Containers []string `toml:"containers"`

	RunState RunState `toml:"run_state"`

	EtcdContainerName       string   `toml:"etcd_container_name"`
	ControllerContainerName string   `toml:"controller_container_name"`
	RegistryContainerName   string   `toml:"registry_container_name"`
	WorkerContainerNames    []string `toml:"worker_container_names"`

	StorageDriver string `toml:"storage_driver"`
	StoragePool   string `toml:"storage_pool"`
}
