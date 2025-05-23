<p align="center">
  <img src="https://img.shields.io/github/v/tag/xxddpac/go-flow?label=version" alt="version" />
  <img src="https://img.shields.io/github/license/xxddpac/go-flow" alt="license" />
  <img src="https://img.shields.io/badge/Go-1.21-blue" alt="Go version" />
  <img src="https://img.shields.io/github/stars/xxddpac/go-flow?style=social" alt="GitHub stars" />
  <img src="https://img.shields.io/github/last-commit/xxddpac/go-flow" alt="last commit" />
</p>


<h1 align="center">🌐 GO-FLOW</h1>

<p align="center"><strong>轻量级、高性能的实时网络流量监控与异常检测工具</strong></p>

<p align="center">
部署即用，无需依赖，提供 Web 控制台与告警机制，可快速定位异常流量、扫描行为等潜在风险。
</p>

## ✨ 项目概述

- 🚀 即装即用，零依赖部署
- 📊 实时流量监控：会话/IP/端口级别流量排名
- 📉 流量趋势图：自动生成滑动窗口视图
- 🚨 异常告警机制：识别大流量、高频扫描等异常行为
---

## 🧩 使用场景

### 1. 本机流量分析

```
部署在主机或容器中，监控自身网络流量，适用于服务节点、办公终端、云主机等环境
```

### 2. 核心链路流量旁路分析

```
通过交换机端口镜像（Port Mirror / SPAN）或网络 TAP，将边缘设备、网关、防火墙等关键链路流量引导至 go-flow，进行实时分析与异常检测
```

---

## 🚀 快速开始

### 1. 下载最新release包

从 [Releases 页面](https://github.com/xxddpac/go-flow/releases) 下载适合系统的可执行文件。

### 2. 配置 `config.toml`

配置网卡
```toml
[server]
Eth = "em0"
```

### 3. 启动服务并访问控制台

```
# Linux
chmod +x go-flow
./go-flow -c config.toml

# Windows
go-flow.exe -c config.toml
```

访问 `http://<server_ip>:31415` 查看控制台页面

## 📊 Web UI

- `Sessions` 展示会话级流量，包含源/目的 IP、端口、协议、流量占用和请求次数，便于分析具体通信关系

![Flows](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/flows.jpg)

- `IP Stats` 展示单个 IP 的流量使用和请求次数，帮助识别高流量主机和活跃节点

![IPs](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/ips.jpg)

- `Port Stats` 展示各目的端口的流量占用和协议分布，便于识别热门服务和异常端口活动

![Ports](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/ports.jpg)

- `Trend` 展示滑动时间窗口内整体网络流量变化趋势，便于观察流量高峰、波动和异常增长情况

![Trend](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/trend.jpg)

## 🚨 风险预警

- `大流量预警` 计算滑动窗口内的流量总和，识别异常大流量
  ![Bandwidth](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/bandwidth.jpg)

- `高频扫描预警` 识别高频扫描或分布式探测
  ![Frequency](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/frequency.jpg)

## 💾 数据持久化与扩展

`go-flow` 设计初衷是**简单轻量、开箱即用**，默认采用内存结构，在滑动时间窗口内完成**实时流量分析与异常检测**，无需依赖任何外部组件。

如需进一步实现**数据持久化**与**高级分析功能**，例如：

- 查看最近一周或一个月的流量趋势
- 生成更丰富的统计图表与报表
- 结合威胁情报进行行为关联分析

可通过配置文件启用 **Kafka**，将分析结果写入消息队列，供后续系统自由消费与处理。

```
[kafka]
Enable = true
Brokers = ["localhost:9092"]
Topic = "go-flow"
```  

![Dashboard](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/dashboard.jpg)

## 🛠️ 源码编译

```
git clone https://github.com/xxddpac/go-flow.git
cd go-flow

# Build for Linux
make build-linux

# Build for Windows
make build-windows

# Clean build artifacts
make clean
```