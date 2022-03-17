[Unit]
Description=oci-registry

[Service]
ExecStart=/usr/local/bin/registry serve /etc/docker/registry/config.yml
Restart=on-failure
RestartSec=5
