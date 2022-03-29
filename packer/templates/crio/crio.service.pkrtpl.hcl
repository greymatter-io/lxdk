[Unit]
Description=CRI-O daemon

[Service]
ExecStart=/usr/local/bin/crio --registry docker.io
Restart=always
RestartSec=10s

[Install]
WantedBy=multi-user.target
