# 多阶段构建：先在 golang 1.24 镜像中编译，再放入精简运行镜像

FROM golang:1.24-alpine AS builder

WORKDIR /app

# 预先拷贝依赖文件，加快构建缓存
COPY go.mod go.sum ./
RUN go mod download

# 拷贝源码
COPY . .

# 构建静态二进制，目标平台为 linux/amd64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o google-proxy .

# 运行阶段
FROM alpine:3.20

WORKDIR /app

# 安装 CA 证书，用于访问 HTTPS（例如 translate.google.com）
RUN apk add --no-cache ca-certificates

# 拷贝编译好的二进制
COPY --from=builder /app/google-proxy /app/google-proxy

# 默认暴露 8080 端口
EXPOSE 8080

# 运行时可以通过环境变量 SOCKS5_URL 控制上游 SOCKS5 代理
# 例如：SOCKS5_URL="socks5://user:pass@1.2.3.4:1080"

ENTRYPOINT ["/app/google-proxy"]
