module github.com/greymatter-io/lxdk

replace google.golang.org/grpc/naming => google.golang.org/grpc v1.29.1

go 1.16

require (
	github.com/BurntSushi/toml v0.4.1
	github.com/go-git/go-billy/v5 v5.3.1
	github.com/go-git/go-git/v5 v5.4.2
	github.com/lxc/lxd v0.0.0-20220323040909-6ecd7aa631e8
	github.com/pkg/errors v0.9.1
	github.com/urfave/cli/v2 v2.3.0
	golang.org/x/crypto v0.0.0-20220307211146-efcb8507fb70 // indirect
	k8s.io/apimachinery v0.23.5
	k8s.io/client-go v0.23.5
)
