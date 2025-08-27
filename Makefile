# 定义变量
SERVER_BIN := bin/code_server
PUBLISHER_BIN := bin/task_publisher
EXECUTER_BIN := bin/task_executer
CONFIG_HTML := cmd/task_executor/config.html
COPIED_HTML := $(dir $(EXECUTER_BIN))/config.html

# 默认目标
all: $(SERVER_BIN) $(PUBLISHER_BIN) $(EXECUTER_BIN) $(COPIED_HTML)

# 创建输出目录
$(shell mkdir -p bin/server bin/publisher bin/executer)

# 构建 code_server
$(SERVER_BIN): cmd/code_server/main.go
	go build -o $(SERVER_BIN) ./cmd/code_server

# 构建 task_publisher
$(PUBLISHER_BIN): cmd/task_publisher/task_publisher.go
	go build -o $(PUBLISHER_BIN) ./cmd/task_publisher

# 构建 task_executer
$(EXECUTER_BIN): cmd/task_executor/task_executer.go
	go build -o $(EXECUTER_BIN) ./cmd/task_executor
	@mkdir -p $(dir $(EXECUTER_BIN))

# 复制html文件
$(COPIED_HTML): $(CONFIG_HTML)
	@mkdir -p $(dir $(COPIED_HTML))
	@cp $(CONFIG_HTML) $(COPIED_HTML)

# 清理构建产物
clean:
	rm -rf bin/

# 安装依赖
install:
	go mod tidy

.PHONY: all clean install