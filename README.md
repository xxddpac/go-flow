# GO-FLOW

## 介绍

go-flow 是一款轻量级、高性能的实时网络流量监控与异常检测工具。
基于可配置的滑动时间窗口进行流量分析，并提供简洁直观的 Web 控制台，方便实时查看网络态势。
内置告警机制可快速识别异常大流量、高频扫描、分布式探测等可疑行为并触发告警，提前预警潜在安全风险。

## 使用

```
# 下载最新的Release版本
./go-flow -c config.toml
# 具体配置见 config.toml
```

## WEB控制台

- `Sessions` 展示会话级流量，包含源/目的 IP、端口、协议、流量占用和请求次数，便于分析具体通信关系

![Flows](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/flows.jpg)

- `IP Stats` 展示单个 IP 的流量使用和请求次数，帮助识别高流量主机和活跃节点

![IPs](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/ips.jpg)

- `Port Stats` 展示各目的端口的流量占用和协议分布，便于识别热门服务和异常端口活动

![Ports](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/ports.jpg)

- `Trend` 展示滑动时间窗口内整体网络流量变化趋势，便于观察流量高峰、波动和异常增长情况

![Trend](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/trend.jpg)

## 预警 (邮件/企微)

![Bandwidth](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/bandwidth.jpg)

![Frequency](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/frequency.jpg)

## 编译

```
git clone https://github.com/xxddpac/go-flow.git
cd go-flow

# Build for CentOS
make build-centos

# Build for Windows
make build-win

# Clean build artifacts
make clean
```