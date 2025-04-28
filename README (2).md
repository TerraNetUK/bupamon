# bupamon 
## fping latency monitor exporter to InfluxDb

Parses fping output, and sends the results to InfluxDb 2.x for visualisation in Grafana.

##### Prerequisites
- golang 1.24.2 or newer
- fping 
- InfluxDB 2.x
```
$ influx bucket create -n infping -r 0
ID			Name	Retention	Shard group duration	Organization ID		Schema Type
4f5dab66955ddf7d	infping	infinite	168h0m0s		785274777f4f03a4	implicit

# Create auth tokens for write and read
jb@mobihex:~/bin$ influx auth create --org acme --write-bucket 4f5dab66955ddf7d
ID			Description	Token												User Name	User ID			Permissions
092aee8b39223000			<WRITE Token>	jb		092aedce4a623000	[write:orgs/785274777f4f03a4/buckets/4f5dab66955ddf7d]
jb@mobihex:~/bin$ influx auth create --org acme --read-bucket 4f5dab66955ddf7d
ID			Description	Token												User Name	User ID			Permissions
092af66d1be23000			<READ Token>	jb		092aedce4a623000	[read:orgs/785274777f4f03a4/buckets/4f5dab66955ddf7d]
```

#### Edit config.yaml:
Check the path to the fping binary.
For Docker containers: If the service isn't able to create the logfile at the configured location, output will be re-directed to stdout, which is preferred for containers.

#### Install infping:

##### Systemd

```
$ ./setup.sh
$ sudo systemctl status infping.service

```
##### Docker container

```
$ cp config.toml.example config.toml
# Adjust config.toml file
$ vi config.toml
# Create binary
$ go mod tidy
$ CGO_ENABLED=0 go build
# Build container
$ docker build . -t sinnohd/infping:0.2.0
$ docker run sinnohd/infping:0.2.0

```


#### Output
```
Feb 24 15:14:30 ip-172-19-64-10 infping: 2021/02/24 15:14:30 Connected to influxdb! (dur:9.877542ms, ver:1.8.0)
Feb 24 15:14:30 ip-172-19-64-10 infping: 2021/02/24 15:14:30 Going to ping the following ips: [192.168.0.1 192.168.0.2]
Feb 24 15:14:40 ip-172-19-64-10 infping: 2021/02/24 15:14:40 IP:192.168.0.1, send:10, recv:10 loss: 0, min: 1.95, avg: 2.13, max: 2.70
Feb 24 15:14:40 ip-172-19-64-10 infping: 2021/02/24 15:14:40 IP:192.168.0.2, send:10, recv:10 loss: 0, min: 289, avg: 289, max: 291
```

#### Todo
Replace call to fping binary with [go-ping](https://github.com/go-ping/ping) lib.

