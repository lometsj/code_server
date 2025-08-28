# Code Server

一个基于Go语言的代码分析服务器系统，提供代码符号查询、引用分析和LLM集成功能。

## 项目结构

```
code_server/
├── cmd/                    # 主要命令行工具
│   ├── code_server/        # 代码分析服务器
│   ├── task_publisher/     # 任务发布器
│   └── task_executor/      # 任务执行器
├── static_binary/          # 嵌入的二进制工具
│   └── linux/             # Linux平台的二进制文件
├── static/                # 静态资源
│   └── config/           # 配置文件
├── prompts/              # LLM提示词模板
├── results/              # 任务执行结果
└── html/                 # HTML配置界面
```

## 二进制文件说明

### 1. code_server
**路径**: `bin/code_server`
**用途**: 代码分析服务器，提供代码符号查询和引用分析功能

**功能**:
- 提供HTTP API接口进行代码符号查询
- 支持符号信息获取（GetSymbolInfo）
- 支持符号引用查找（FindAllRefs）
- 集成ctags、readtags、global、gtags等代码分析工具
- 内置嵌入式二进制文件，无需外部依赖

**启动方式**:
```bash
cd /path/to/code/dir
find . -type f \( -name "*.c" -o -name "*.h" \) > filelist
mkdir .tsj
ctags -L filelist -o .tsj/tags
gtags -i -f filelist -o .tsj
./bin/code_server
```

**API接口**:
- `POST /api/get_symbol` - 获取符号信息
- `POST /api/find_refs` - 查找符号引用

### 2. task_publisher
**路径**: `bin/task_publisher`
**用途**: 任务发布器，用于向任务执行器提交代码分析任务

**功能**:
- 提交代码分析任务到task_executor
- 支持多种预定义的问题类型（如敏感信息泄露检测）
- 管理LLM配置和代码服务器配置
- 提供任务状态查询和等待功能

**使用方式**:
```bash
./bin/task_publisher
```

**主要操作**:
- 提交任务到执行器
- 获取执行器配置
- 查询任务状态
- 等待任务完成

### 3. task_executor
**路径**: `bin/task_executer`
**用途**: 任务执行器，接收并执行代码分析任务

**功能**:
- 提供Web界面进行配置管理
- 接收task_publisher提交的任务
- 集成LLM进行智能代码分析
- 管理任务执行结果
- 支持多种代码分析场景

**启动方式**:
```bash
./bin/task_executer
```

**Web界面**: 启动后可通过浏览器访问配置界面

## 嵌入式二进制工具

项目包含以下嵌入式二进制工具，用于代码分析：

### ctags
**用途**: 生成代码标签文件，提取代码结构信息
**功能**: 解析源代码，生成函数、变量、宏等符号信息

### readtags
**用途**: 读取和查询ctags生成的标签文件
**功能**: 快速查找符号定义和位置信息

### global
**用途**: 全局符号查找工具
**功能**: 在代码库中进行跨文件的符号搜索和引用分析

### gtags
**用途**: 生成全局标签文件
**功能**: 创建用于global工具的索引文件，支持快速代码导航

## 配置文件

### LLM配置 (static/config/config.json)
```json
{
  "llm_configs": [
    {
      "name": "qwen3-30b",
      "api_key": "your_api_key",
      "base_url": "http://your_llm_server:port/v1",
      "model": "qwen3-32b"
    }
  ],
  "code_servers": [
    {
      "name": "test_c_file",
      "url": "127.0.0.1:46538"
    }
  ]
}
```

### 提示词模板 (prompts/)
- `sensitive_leak.json`: 敏感信息泄露检测的提示词模板

## 构建和部署

### 构建所有二进制文件
```bash
make all
```

### 调试模式构建
```bash
make debug
```

### 生产模式构建
```bash
make release
```

### 打包部署
```bash
make package
```

### 清理构建产物
```bash
make clean
```

## 使用流程

1. **启动code_server**
   ```bash
   ./bin/code_server
   ```
   服务器将在默认端口启动，提供代码分析API

2. **启动task_executor**
   ```bash
   ./bin/task_executer
   ```
   启动任务执行器，可通过Web界面进行配置

3. **使用task_publisher提交任务**
   ```bash
   ./bin/task_publisher
   ```
   根据提示选择任务类型和参数，提交分析任务

4. **查看结果**
   任务执行结果将保存在`results/`目录中

## 主要应用场景

- 代码符号查询和分析
- 敏感信息泄露检测
- 代码质量分析
- 函数调用关系分析
- 智能代码审查

## 依赖要求

- Go 1.22.2 或更高版本
- Linux操作系统（嵌入式二进制工具针对Linux平台）

## 注意事项

- 所有嵌入式二进制工具都打包在可执行文件中，无需额外安装
- 配置文件中的API密钥和服务器地址需要根据实际环境修改
- 任务执行器需要正确的LLM配置才能正常工作