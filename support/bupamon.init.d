#!/sbin/openrc-run
# Provides:          bupamon
# Required-Start:    $network $syslog
# Required-Stop:     $network $syslog
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6

# Basic service information
name="BupaMon exporter"
description="Bupamon exporter for latency metrics to InfluxDB"
command="/srv/bupamon/bupamon"
command_background=true
pidfile="/run/bupamon.pid"

# Working directory setting
directory="/srv/bupamon"

# Dependencies
depend() {
    need net
    after firewall
    use logger dns
}
