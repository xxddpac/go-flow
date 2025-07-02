<p align="center"><em>轻量级高性能网络流量分析工具</em></p>

---

### 项目简介

go-flow 是一款轻量级、高性能的网络流量分析工具，基于 Go 语言开发，结合高效的数据处理和低资源占用，适用于边缘计算节点到高吞吐数据中心的多种场景。

---

### 核心功能

- 滑动窗口分析：实时统计指定时间窗口内的 IP、端口和协议流量，支持 Top-N 排序，便于快速定位流量热点。
- 异常检测与告警：自动检测大流量、高频请求等异常行为，提供灵活的告警配置。
- Kafka 数据流输出：支持将分析结果异步推送至 Kafka，无缝集成 ELK Stack、Grafana 等可视化平台。

---

### 安装与使用

#### 下载 release 包

前往 [Releases](https://github.com/xxddpac/go-flow/releases) 下载最新版本压缩文件。

#### 编辑配置文件

```toml
Eth = "eth0" # 监听的网卡名
```
#### 启动服务
```
chmod +x go-flow
./go-flow -c config.toml
```
#### WEB访问
```
http://server_ip:31415
```
---

### ScreenShot

![Ui](https://raw.githubusercontent.com/xxddpac/go-flow/main/image/ui.jpg)

---

### 源码编译

```
git clone https://github.com/xxddpac/go-flow.git
cd go-flow
make build
```
---

### 部署与性能常见问题

#### Q: 对主机资源占用情况如何？

A: `go-flow` 使用了如下优化设计以降低资源占用：

- 高效数据结构：采用堆排序和滑动窗口机制，高效完成流量聚合。

- 协程池管理：动态调度 Goroutine，避免资源泄漏。

- 内存缓存：减少重复计算，提升处理效率。

#### Q: 能支持多大的网络流量？性能如何？

A: `go-flow` 使用 Linux 原生的 `AF_PACKET` 零拷贝技术，在内核态完成数据捕获和复制，数据通过共享内存直达用户态：
- 性能上限：理论支持网卡带宽的最大吞吐量。
- 高可靠性：内核态零拷贝确保数据捕获无丢失。

在实测网络环境中（4Gbps 带宽、约2万终端），go-flow 运行稳定，未发现丢包。

> ⚠️ **注意**
> 虽然内核采集是零拷贝高性能的，但在用户态中程序仍需处理流量聚合、滑动窗口维护、堆排序等操作，尤其是在超高并发流量下，如果用户空间处理速度跟不上，仍有概率导致丢包。
>
> 可通过以下方式优化：
> - 增加协程池容量（`config.toml` 中的 `Workers` 参数）
> - 调整生产者通道缓冲区（`config.toml` 中的 `PacketChan` 参数）
> - 延迟缓存更新频率（`config.toml` 中的 `Interval` 参数）
> - 使用 pprof 分析热点函数，默认监听端口`31414`，访问 `http://server_ip:31414/debug/pprof/` 查看性能分析数据

另外通过日志可观察丢包情况，默认日志路径`/var/log/go_flow.log`

#### Q:为什么选择 AF_PACKET 而非 eBPF？

A: 相较于 `eBPF`，`go-flow` 选择使用 `AF_PACKET` 的原因主要有以下几点：

- 开发效率：`AF_PACKET` 是 `Linux` 原生抓包接口，配合 `TPACKETv3` 实现零拷贝，无需开发复杂的 `eBPF` 程序，调试更高效。
- 广泛兼容性：支持大多数 `Linux` 系统，无需特定内核版本或额外模块，部署简单可靠。

#### Q: 为什么最新版只提供了 Linux 版本？

A: `go-flow` 使用的核心采集机制是 `AF_PACKET`，这是 `Linux` 内核特有的高性能网络捕获机制，不支持 `Windows` 平台。
如需在 `Windows` 上体验功能，可下载 `v1.2.1` 版本中的 `go_flow_windows.zip`，该版本基于 `libpcap` 构建。