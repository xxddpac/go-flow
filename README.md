# go-flow

[中文文档（Chinese Docs）](https://github.com/xxddpac/go-flow/blob/main/README_ZH.md)

## Overview

go-flow is a lightweight and efficient real-time network traffic monitoring tool, ideal for medium-scale traffic
environments. It captures TCP/UDP packets and analyzes traffic using a configurable sliding time window. Designed for
simplicity and performance, it also includes a built-in web UI for intuitive, real-time visualization.

## Usage

```
# Download the binary from new latest release
./go-flow -c config.toml
```

## Screenshot

- `Flows` show the top N flows detail in sliding window

![Flows](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/flows.jpg)

- `IPs` show the top N IPs use total bandwidth in sliding window

![IPs](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/ips.jpg)

- `Ports` show the top N Ports use total bandwidth in sliding window

![Ports](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/ports.jpg)

- `Trend` show the Trend of the bandwidth in sliding window

![Trend](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/trend.jpg)

- `Alert` alert when the bandwidth exceed the threshold (Mail/WeCom)
![Alert](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/alert.jpg)

## Build

```
git clone https://github.com/xxddpac/go-flow.git
cd go-flow

# Build for CentOS
make build-centos
# Output: bin/go-flow

# Build for Windows
make build-win
# Output: bin/go-flow.exe

# Clean build artifacts
make clean
```