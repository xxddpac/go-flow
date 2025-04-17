# go-flow

## Overview

go-flow is a real-time network traffic monitoring and DDoS detection tool that captures TCP/UDP packets and ranks the top 10
IPs within a configurable sliding window

## Usage

```
chmod +x bin/go-flow
./bin/go-flow --eth=<network interface> --size=<window size> default size = 5(minute)

```

## Example

```
./bin/go-flow --eth=eth0

# and then visit http://ip_address:31415

```

## Screenshot

<img src="gf.png" alt="">

## Build

```
make build
```