## 多阶段构建，生成体积较小的运行镜像

# ---------- 构建阶段 ----------
FROM golang:1.24-alpine AS builder

WORKDIR /app

# 可选：安装常用工具（如需要拉取私有仓库可以打开 git）
# 这里主要保证 CA 证书正常，便于拉取依赖
RUN apk add --no-cache ca-certificates

# 先复制 go.mod / go.sum，加快构建缓存
COPY go.mod go.sum ./
RUN go mod download

# 再复制剩余源码
COPY . .

# 编译为静态 Linux amd64 二进制
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o google-proxy .


# ---------- 运行阶段 ----------
FROM alpine:3.20

WORKDIR /app

# 安装 CA 证书和时区数据，保证 HTTPS 以及 TZ=Asia/Shanghai 正常
RUN apk add --no-cache ca-certificates tzdata && \
    mkdir -p /app/logs

# 默认时区，可被环境变量覆盖
ENV TZ=Asia/Shanghai

# 拷贝编译好的二进制
COPY --from=builder /app/google-proxy /app/google-proxy

# 服务监听端口
EXPOSE 8080

# 入口命令
CMD ["/app/google-proxy"]

