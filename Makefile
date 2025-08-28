# 定义变量
SERVER_BIN := bin/code_server
PUBLISHER_BIN := bin/task_publisher
EXECUTER_BIN := bin/task_executer
CONFIG_HTML := cmd/task_executor/config.html
COPIED_HTML := $(dir $(EXECUTER_BIN))/config.html

# 打包相关变量
PACKAGE_NAME := task_executor_package
PACKAGE_DIR := dist/$(PACKAGE_NAME)
TARBALL_NAME := $(PACKAGE_NAME).tar.gz

# 默认目标
all: $(SERVER_BIN) $(PUBLISHER_BIN) $(EXECUTER_BIN) $(COPIED_HTML)

# 创建输出目录
$(shell mkdir -p bin/server bin/publisher bin/executer dist)

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

# 调试模式构建（包含调试信息）
debug: cmd/task_executor/task_executer.go
	go build -gcflags="all=-N -l" -o $(EXECUTER_BIN) ./cmd/task_executor
	@mkdir -p $(dir $(EXECUTER_BIN))
	@cp $(CONFIG_HTML) $(COPIED_HTML)

# 生产模式构建（优化编译）
release: cmd/task_executor/task_executer.go
	go build -ldflags="-s -w" -o $(EXECUTER_BIN) ./cmd/task_executor
	@mkdir -p $(dir $(EXECUTER_BIN))
	@cp $(CONFIG_HTML) $(COPIED_HTML)

# 准备打包目录
prepare-package: $(EXECUTER_BIN) $(COPIED_HTML)
	@rm -rf $(PACKAGE_DIR)
	@mkdir -p $(PACKAGE_DIR)/static
	@mkdir -p $(PACKAGE_DIR)/prompts
	@mkdir -p $(PACKAGE_DIR)/results
	@cp $(EXECUTER_BIN) $(PACKAGE_DIR)/
	@cp $(COPIED_HTML) $(PACKAGE_DIR)/
	@cp static/config/config.json $(PACKAGE_DIR)/ 2>/dev/null || true
	@cp -r static/* $(PACKAGE_DIR)/static/ 2>/dev/null || true
	@cp -r prompts/* $(PACKAGE_DIR)/prompts/ 2>/dev/null || true
	@echo "Package prepared in: $(PACKAGE_DIR)"

# 创建打包文件
package: prepare-package
	@cd dist && tar -czf $(TARBALL_NAME) $(PACKAGE_NAME)/
	@echo "Package created: dist/$(TARBALL_NAME)"

# 清理构建产物
clean:
	rm -rf bin/ dist/

# 安装依赖
install:
	go mod tidy

# 运行调试模式（开发环境）
debug-run: debug
	./$(EXECUTER_BIN)

# 运行生产模式（生产环境）
release-run: release
	./$(EXECUTER_BIN)

# 测试打包内容
test-package: package
	@mkdir -p test_package
	@cd test_package && tar -xzf ../dist/$(TARBALL_NAME)
	@echo "Testing package in test_package/$(PACKAGE_NAME)/"
	@cd test_package/$(PACKAGE_NAME) && ls -la
	@echo "Package test completed. Clean up with: rm -rf test_package"

.PHONY: all clean install debug release package prepare-package debug-run release-run test-package