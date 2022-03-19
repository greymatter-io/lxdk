module github.com/greymatter-io/lxdk

replace google.golang.org/grpc/naming => google.golang.org/grpc v1.29.1

go 1.16

require (
	github.com/BurntSushi/toml v0.4.1
	github.com/lxc/lxd v0.0.0-20220211231312-81512e0826f1
	github.com/pkg/errors v0.9.1
	github.com/urfave/cli/v2 v2.3.0
	golang.org/x/net v0.0.0-20220127200216-cd36cc0744dd
	k8s.io/apimachinery v0.23.5
	k8s.io/client-go v0.23.5
)
