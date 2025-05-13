<p align="center">
  <img src="https://img.shields.io/github/v/tag/xxddpac/go-flow?label=version" alt="version" />
  <img src="https://img.shields.io/github/license/xxddpac/go-flow" alt="license" />
  <img src="https://img.shields.io/badge/Go-1.21-blue" alt="Go version" />
  <img src="https://img.shields.io/github/stars/xxddpac/go-flow?style=social" alt="GitHub stars" />
  <img src="https://img.shields.io/github/last-commit/xxddpac/go-flow" alt="last commit" />
</p>


<h1 align="center">GO-FLOW</h1>

<p align="center">
  <strong>轻量级、高性能的实时网络流量监控与异常检测工具</strong>
</p>

<p align="center">
基于可配置的滑动时间窗口进行流量分析，并提供简洁直观的 Web 控制台，方便实时查看网络态势。
内置告警机制可快速识别异常大流量、高频扫描、分布式探测等可疑行为并触发告警，提前预警潜在安全风险。
</p>


🚀 <strong>快速上手</strong>

- 下载最新版本Release
- 编辑配置文件 `config.toml`
- 运行 `go-flow`，访问 `http://ip:31415`

```
./go-flow -c config.toml
```

📊 <strong>Web UI</strong>

- `Sessions` 展示会话级流量，包含源/目的 IP、端口、协议、流量占用和请求次数，便于分析具体通信关系

![Flows](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/flows.jpg)

- `IP Stats` 展示单个 IP 的流量使用和请求次数，帮助识别高流量主机和活跃节点

![IPs](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/ips.jpg)

- `Port Stats` 展示各目的端口的流量占用和协议分布，便于识别热门服务和异常端口活动

![Ports](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/ports.jpg)

- `Trend` 展示滑动时间窗口内整体网络流量变化趋势，便于观察流量高峰、波动和异常增长情况

![Trend](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/trend.jpg)

🚨 <strong>风险预警</strong>

- `大流量预警` 计算滑动窗口内的流量总和，识别异常大流量
  ![Bandwidth](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/bandwidth.jpg)

- `高频扫描预警` 识别高频扫描或分布式探测
  ![Frequency](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/frequency.jpg)

💾 <strong>数据持久化</strong>

`go-flow` 设计初衷是简单轻量化，无需依赖额外组件，默认基于给定滑动窗口在内存中完成实时分析。
如果需要持久化数据以实现更多功能（如查看最近一周或一个月的流量趋势、生成丰富统计图、结合威胁情报等），`go-flow` 也支持将流量数据写入
Kafka 队列，供自定义消费与处理。
只需在配置文件中启用 Kafka 即可，后续的数据存储与分析自行实现。
![Dashboard](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/dashboard.jpg)

🛠️ <strong>源码编译</strong>

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