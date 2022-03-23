package lxd

import (
	lxd "github.com/lxc/lxd/client"
	"github.com/pkg/errors"
)

const snapSocketPath = "/var/snap/lxd/common/lxd/unix.socket"

func InstanceServerConnect() (lxd.InstanceServer, error) {
	var is lxd.InstanceServer
	is, err := lxd.ConnectLXDUnix("", nil)
	if err != nil {
		is, err = lxd.ConnectLXDUnix(snapSocketPath, nil)
		if err != nil {
			return lxd.InstanceServer(is), errors.Errorf("could not connect to socket at default location or %s", snapSocketPath)
		}
	}

	return is, nil
}
