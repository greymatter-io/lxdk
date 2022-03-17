[Unit]
Description=CRI-O daemon

[Service]
ExecStartPre=/usr/bin/mkdir -p /run/kata-containers/shared/sandboxes
ExecStartPre=/usr/bin/mount --bind --make-rshared /run/kata-containers/shared/sandboxes /run/kata-containers/shared/sandboxes
ExecStart=/usr/local/bin/crio
Restart=always
RestartSec=10s

[Install]
WantedBy=multi-user.target
