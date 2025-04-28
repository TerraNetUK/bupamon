#!/bin/bash
# Print work dir
pwd="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Go install
go build -o bupamon

# Create bupamon.service

cat <<'EOF' > bupamon.service
[Unit]
Description=bupamon
Requires=network-online.target
After=network-online.target consul.service

[Service]
Type=idle
User=root
Group=root
WorkingDirectory=$pwd
PIDFile=/var/run/bupamon.pid
ExecStart=$pwd/bupamon
ExecReload=/bin/kill -HUP $MAINPID
KillSignal=SIGINT

# Give a reasonable amount of time for the server to start up/shut down
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sed -i "s|WorkingDirectory.*|WorkingDirectory=$pwd|g" $pwd/bupamon.service
sed -i "s|ExecStart.*|ExecStart=$pwd/bupamon|g" $pwd/bupamon.service

# Create systemd bupamon.service
sudo mv $pwd/bupamon.service /etc/systemd/system/bupamon.service
sudo systemctl daemon-reload
sudo systemctl enable bupamon.service
sudo systemctl start bupamon.service
