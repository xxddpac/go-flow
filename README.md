# go-flow

## Overview

go-flow is a lightweight, high-performance tool for real-time network traffic monitoring and DDoS detection. It captures TCP/UDP packets and analyzes within a configurable sliding time window.

## Usage

```
# For centos
#Ensure libpcap installed
# sudo yum install libpcap -y
chmod +x go-flow
./go-flow --eth=<network interface> --size=<window size>

#--eth: Network interface to monitor (e.g., eth0 on CentOS, \Device\NPF_{...} on Windows).
#--size: Sliding window size in minutes (default: 5).

# For windows
./go-flow.exe --eth=<network interface> --size=<window size>
```

## Example

```
./go-flow --eth=eth0
# and then visit http://ip_address:31415

```

## Screenshot

<img src="gf.png" alt="">

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