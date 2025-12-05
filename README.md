# google-proxy

一个针对 Google Translate 的高性能反向代理，支持可选 SOCKS5 上游代理、IP 级限流、按天分割访问日志。

## 功能特性

- 反向代理到 `https://translate.google.com`
- 可选 SOCKS5 上游代理（通过环境变量配置）
- 高性能转发：
  - 自定义 `http.Transport` 连接池参数
  - 启用 HTTP/2（`ForceAttemptHTTP2`）
  - 使用 `BufferPool` 降低内存分配和 GC 压力
- 访问日志：
  - 按天分割日志文件，写入 `logs/access-YYYY-MM-DD.log`
  - 只记录访问 IP 和 UA
  - 异步写盘，避免磁盘 IO 阻塞请求
- 系统/错误日志：
  - 写入 `logs/error-YYYY-MM-DD.log`
  - 启动信息、SOCKS5 配置、FATAL 等都会记录
- 简单 IP 限流：
  - 默认每个 IP 每 10 秒最多 300 次请求（约 30 QPS）
  - 纯内存令牌桶实现，无外部依赖

## 编译与运行

### 使用 Docker（推荐）

在项目根目录构建镜像：

```bash
docker build -t google-proxy .
```

直接运行（直连，不配置 SOCKS5）：

```bash
docker run --rm -p 8080:8080 google-proxy
```

带 SOCKS5 上游代理运行示例：

```bash
docker run --rm -p 8080:8080 \
  -e SOCKS5_URL="socks5://user:pass@1.2.3.4:1080" \
  google-proxy
```

### 本地运行（Go 环境）

确保本机已安装 Go（版本需满足 `go.mod` 要求），然后在项目根目录执行：

```bash
go run main.go
```

服务默认监听在 `:8080`。

或者先编译再运行：

```bash
go build -o google-proxy .
./google-proxy
```

### 使用 Makefile 交叉编译

项目提供了简单的多平台打包命令：

```bash
make ubuntu        # 为 Linux amd64 打包（兼容旧用法）
make linux-amd64   # Linux amd64
make linux-arm64   # Linux arm64
make darwin-amd64  # macOS Intel
make darwin-arm64  # macOS Apple Silicon
make windows-amd64 # Windows amd64
```

生成的二进制会放在 `build/` 目录下，上传到目标机器后直接运行即可。

## 环境变量配置

### SOCKS5 上游代理

可通过环境变量 `SOCKS5_URL` 配置上游 SOCKS5 代理，例如：

```bash
export SOCKS5_URL="socks5://user:pass@1.2.3.4:1080"
```

说明：

- 未设置 `SOCKS5_URL` 时：
  - 直接连目标站点
  - 仍然会读取系统 HTTP 代理（`HTTP_PROXY` / `HTTPS_PROXY` 等），这是 Go 的默认行为
- 设置为 `socks5://...` 时：
  - 所有上游连接通过 SOCKS5 转发
  - 不再叠加 HTTP 代理

## 日志说明

程序启动后会自动创建 `logs` 目录，并按天生成两个日志文件：

- 访问日志：`logs/access-YYYY-MM-DD.log`
  - 每个请求一行，格式类似：
  - `2025/01/01 12:00:00 [INFO] access: ip=1.2.3.4 ua="Mozilla/5.0 ..."`
- 系统/错误日志：`logs/error-YYYY-MM-DD.log`
  - 记录 `.env` 加载结果、SOCKS5 配置、FATAL 错误等

如果日志文件创建失败，会自动回退到标准输出，避免日志完全丢失。

## 与 Nginx 的优缺点对比

| 对比项       | 本项目（google-proxy）                          | Nginx                                      |
| ------------ | ----------------------------------------------- | ------------------------------------------ |
| 部署复杂度   | 单一 Go 可执行文件，零依赖，直接运行           | 需要安装服务、维护配置文件                 |
| SOCKS5 支持  | 内置 SOCKS5 上游代理支持                        | 需额外模块或再挂一层代理                   |
| 场景适配     | 针对 Google Translate 优化，开箱即用            | 通用反向代理，需要手动按场景调优          |
| 扩展方式     | 通过 Go 代码扩展业务逻辑，灵活但需编译部署     | 通过配置/模块扩展，更适合作为通用网关     |
| 高级特性     | 内置 IP 限流、访问/错误日志分离，功能聚焦      | 负载均衡、静态资源、缓存、WAF 等更全面    |
| 限流粒度     | 单实例内存级 IP 限流，多实例时各实例各自统计   | 可配合共享存储/模块实现全局统一限流       |

## IP 限流策略

项目在内存中实现了一个简单的令牌桶限流器：

- 维度：客户端 IP（通过 `X-Forwarded-For` / `X-Real-IP` / `RemoteAddr` 获取）
- 默认阈值：每个 IP 每 10 秒最多 300 次请求
- 超限时返回：
  - HTTP 状态码：`429 Too Many Requests`
  - 响应体：`Too Many Requests\n`

如需调整限流阈值，可以在 `main.go` 中修改：

```go
var defaultIPLimiter = newIPRateLimiter(300, 10*time.Second)
```

例如：

- 每 10 秒 600 次：`newIPRateLimiter(600, 10*time.Second)`
- 每秒 100 次：`newIPRateLimiter(100, 1*time.Second)`

## 健康检查

服务提供一个简单的健康检查接口：

- 路径：`/healthz`
- 返回：`200 OK`，文本内容类似 `ok 2025-01-01T12:00:00Z`

可用于 Kubernetes / 容器编排的存活检查或探活脚本。

## 注意事项

- 本项目仅用于学习和个人使用，请遵守目标站点（Google）的使用条款。
- 高并发场景下建议：
  - 适当调大系统 `ulimit -n`（文件描述符上限）
  - 根据实际 QPS 调整 `MaxIdleConns` / `MaxIdleConnsPerHost` 和限流参数
