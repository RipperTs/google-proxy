BINARY := google-proxy
BUILD_DIR := build

# 常用平台列表
PLATFORMS := linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64 windows-arm64

.PHONY: all build ubuntu \
	linux-amd64 linux-arm64 \
	darwin-amd64 darwin-arm64 \
	windows-amd64 windows-arm64 \
	clean

# 一次性编译所有常用平台
all: $(PLATFORMS)

# 通用编译（使用当前环境的 GOOS/GOARCH）
# 用法：
#   GOOS=linux GOARCH=amd64 make build
build:
	@mkdir -p $(BUILD_DIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BINARY) .

# 兼容旧用法：一键为 Ubuntu(amd64) 打包可执行文件
ubuntu: linux-amd64

linux-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BINARY)-linux-amd64 .

linux-arm64:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BINARY)-linux-arm64 .

darwin-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 .

darwin-arm64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BINARY)-darwin-arm64 .

windows-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BINARY)-windows-amd64.exe .

windows-arm64:
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BINARY)-windows-arm64.exe .

clean:
	rm -rf $(BUILD_DIR)

