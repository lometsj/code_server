package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Config 配置结构
type Config struct {
	LLMConfigs  []NamedLLMConfig `json:"llm_configs"`
	CodeServers []CodeServer     `json:"code_servers"`
}

// NamedLLMConfig 定义带名称的LLM配置结构
type NamedLLMConfig struct {
	Name    string `json:"name"`
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
}

// CodeServer 定义代码服务器结构
type CodeServer struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Task 任务结构
type Task struct {
	ID             string `json:"id"`
	SystemPrompt   string `json:"system_prompt"`
	UserPrompt     string `json:"user_prompt"`
	CodeServerName string `json:"code_server_name"`
	LLMConfigName  string `json:"llm_config_name"`
}

// TaskResponse 任务提交响应
type TaskResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	TaskID  string `json:"task_id"`
}

// TaskStatusResponse 任务状态响应
type TaskStatusResponse struct {
	Exists bool `json:"exists"`
}

// ProblemType 问题类型定义
type ProblemType struct {
	Name           string
	Description    string
	SystemPrompt   string
	UserPrompt     string
	RequiresBatch  bool // 是否需要批量处理
	RequiresSymbol bool // 是否需要符号参数
}

// TaskPublisher 任务发布器
type TaskPublisher struct {
	ExecutorURL string
	HTTPClient  *http.Client
}

// NewTaskPublisher 创建新的任务发布器
func NewTaskPublisher(executorURL string) *TaskPublisher {
	return &TaskPublisher{
		ExecutorURL: executorURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SubmitTask 提交任务到执行器
func (tp *TaskPublisher) SubmitTask(task Task) (*TaskResponse, error) {
	taskData, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task: %v", err)
	}

	url := fmt.Sprintf("%s/api/submit_task", tp.ExecutorURL)
	resp, err := tp.HTTPClient.Post(url, "application/json", bytes.NewBuffer(taskData))
	if err != nil {
		return nil, fmt.Errorf("failed to submit task: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("task submission failed with status %d: %s", resp.StatusCode, string(body))
	}

	var taskResp TaskResponse
	if err := json.Unmarshal(body, &taskResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	return &taskResp, nil
}

// GetConfig 从执行器获取配置
func (tp *TaskPublisher) GetConfig() (*Config, error) {
	url := fmt.Sprintf("%s/get_config", tp.ExecutorURL)
	resp, err := tp.HTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get config: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get config failed with status %d: %s", resp.StatusCode, string(body))
	}

	var config Config
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %v", err)
	}

	return &config, nil
}

// GetTaskStatus 获取任务状态
func (tp *TaskPublisher) GetTaskStatus(taskID string) (*TaskStatusResponse, error) {
	url := fmt.Sprintf("%s/get_task_status?id=%s", tp.ExecutorURL, taskID)
	resp, err := tp.HTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get task status: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get task status failed with status %d: %s", resp.StatusCode, string(body))
	}

	var statusResp TaskStatusResponse
	if err := json.Unmarshal(body, &statusResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	return &statusResp, nil
}

// WaitForTaskCompletion 等待任务完成
func (tp *TaskPublisher) WaitForTaskCompletion(taskID string, maxRetries int, retryInterval time.Duration) error {
	for i := 0; i < maxRetries; i++ {
		status, err := tp.GetTaskStatus(taskID)
		if err != nil {
			return fmt.Errorf("failed to get task status: %v", err)
		}

		if !status.Exists {
			// 任务不存在，说明已完成
			return nil
		}

		if i < maxRetries-1 {
			time.Sleep(retryInterval)
		}
	}

	return fmt.Errorf("task did not complete within %d retries", maxRetries)
}

// CodeServerClient code_server客户端
type CodeServerClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewCodeServerClient 创建新的code_server客户端
func NewCodeServerClient(baseURL string) *CodeServerClient {
	return &CodeServerClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SymbolInfo 符号信息
type SymbolInfo struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Line    int    `json:"line"`
	End     int    `json:"end"`
	Content string `json:"content"`
	File    string `json:"file"`
	Typeref string `json:"typeref,omitempty"`
}

// SymbolResponse 符号响应
type SymbolResponse struct {
	Status  string       `json:"status"`
	ResList []SymbolInfo `json:"res_list,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// RefResponse 引用响应
type RefResponse struct {
	Callers []string `json:"callers"`
	Error   string   `json:"error,omitempty"`
}

// GetSymbolInfo 获取符号信息
func (csc *CodeServerClient) GetSymbolInfo(symbol string) error {
	reqBody := map[string]string{
		"symbol": symbol,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}

	url := fmt.Sprintf("%s/api/get_symbol", csc.BaseURL)
	resp, err := csc.HTTPClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to get symbol info: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get symbol info failed with status %d: %s", resp.StatusCode, string(body))
	}
	fmt.Print(string(body))

	return nil
}

// FindAllRefs 获取所有引用
func (csc *CodeServerClient) FindAllRefs(symbol string) error {
	reqBody := map[string]string{
		"symbol": symbol,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}

	url := fmt.Sprintf("%s/api/find_refs", csc.BaseURL)
	resp, err := csc.HTTPClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to find refs: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("find refs failed with status %d: %s", resp.StatusCode, string(body))
	}
	fmt.Print(string(body))
	return nil
}

// BatchTaskPublisher 批量任务发布器
type BatchTaskPublisher struct {
	TaskPublisher    *TaskPublisher
	CodeServerClient *CodeServerClient
}

// NewBatchTaskPublisher 创建新的批量任务发布器
func NewBatchTaskPublisher(executorURL, codeServerURL string) *BatchTaskPublisher {
	return &BatchTaskPublisher{
		TaskPublisher:    NewTaskPublisher(executorURL),
		CodeServerClient: NewCodeServerClient(codeServerURL),
	}
}

// BuildPromptFromCallers 基于调用点构建prompt
func (btp *BatchTaskPublisher) BuildPromptFromCallers(symbol string, callers []string, basePrompt string) string {
	var promptBuilder strings.Builder
	promptBuilder.WriteString(basePrompt)
	promptBuilder.WriteString("\n\n=== 符号分析 ===\n")
	promptBuilder.WriteString(fmt.Sprintf("目标符号: %s\n", symbol))
	promptBuilder.WriteString("\n=== 调用点代码 ===\n")

	for i, caller := range callers {
		promptBuilder.WriteString(fmt.Sprintf("\n调用点 %d:\n", i+1))
		promptBuilder.WriteString("```\n")
		promptBuilder.WriteString(caller)
		promptBuilder.WriteString("\n```\n")
	}

	return promptBuilder.String()
}

// WaitForBatchTasksCompletion 等待批量任务完成
func (btp *BatchTaskPublisher) WaitForBatchTasksCompletion(taskID string, maxRetries int, retryInterval time.Duration) error {
	for i := 0; i < maxRetries; i++ {
		status, err := btp.TaskPublisher.GetTaskStatus(taskID)
		if err != nil {
			return fmt.Errorf("failed to get task status: %v", err)
		}

		if !status.Exists {
			// 任务不存在，说明已完成
			return nil
		}

		if i < maxRetries-1 {
			time.Sleep(retryInterval)
		}
	}

	return fmt.Errorf("batch tasks did not complete within %d retries", maxRetries)
}

// ensureURLProtocol ensures that a URL has the proper protocol prefix
func ensureURLProtocol(url string) string {
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return url
	}
	return "http://" + url
}

func main() {
	// 检查是否有足够的参数
	if len(os.Args) < 2 {
		fmt.Printf("Usage:\n")
		fmt.Printf("  task_publisher list llm\n")
		fmt.Printf("  task_publisher list code\n")
		fmt.Printf("  task_publisher submit --system-prompt xxx --user-prompt xxx --code-server xxx --llm-config xxx --id xxx\n")
		fmt.Printf("  task_publisher submit --system-prompt-b64 xxx --user-prompt-b64 xxx --code-server xxx --llm-config xxx --id xxx\n")
		fmt.Printf("  task_publisher get_sym [symbol_name] --code-server name\n")
		fmt.Printf("  task_publisher find_refs [symbol_name] --code-server name\n")
		os.Exit(1)
	}

	// 获取子命令
	subcommand := os.Args[1]

	// 从环境变量获取executor URL
	executorURL := os.Getenv("EXECUTOR_URL")
	if executorURL == "" {
		executorURL = "http://localhost:8080" // 默认值
	}

	// 创建任务发布器
	publisher := NewTaskPublisher(executorURL)

	// 根据子命令处理不同的参数
	switch subcommand {
	case "list":
		if len(os.Args) < 3 {
			fmt.Printf("Usage: task_publisher list [llm|code]\n")
			os.Exit(1)
		}
		listType := os.Args[2]

		// 从executor获取配置
		config, err := publisher.GetConfig()
		if err != nil {
			fmt.Printf("Error getting config from executor: %v\n", err)
			os.Exit(1)
		}

		switch listType {
		case "llm":
			// 列出LLM配置
			fmt.Println("=== LLM Configurations ===")
			for _, llmConfig := range config.LLMConfigs {
				fmt.Printf("%s: %s (%s)\n", llmConfig.Name, llmConfig.Model, llmConfig.BaseURL)
			}
		case "code":
			// 列出CodeServer配置
			fmt.Println("=== Code Server Configurations ===")
			for _, codeServer := range config.CodeServers {
				fmt.Printf("%s: %s\n", codeServer.Name, codeServer.URL)
			}
		default:
			fmt.Printf("Error: unknown list type '%s'\n", listType)
			fmt.Printf("Available list types: llm, code\n")
			os.Exit(1)
		}

	case "submit":
		// 解析submit命令的参数
		flagSet := flag.NewFlagSet("submit", flag.ExitOnError)
		systemPrompt := flagSet.String("system-prompt", "", "System prompt for the task")
		userPrompt := flagSet.String("user-prompt", "", "User prompt for the task")
		systemPromptB64 := flagSet.String("system-prompt-b64", "", "System prompt in base64")
		userPromptB64 := flagSet.String("user-prompt-b64", "", "User prompt in base64")
		codeServerName := flagSet.String("code-server", "default", "Code server name")
		llmConfigName := flagSet.String("llm-config", "default", "LLM configuration name")
		id := flagSet.String("id", "", "Task ID")

		// 解析参数，跳过前两个参数（程序名和子命令）
		flagSet.Parse(os.Args[2:])

		// 处理base64编码的参数
		finalSystemPrompt := *systemPrompt
		finalUserPrompt := *userPrompt

		if *systemPromptB64 != "" {
			decoded, err := base64.StdEncoding.DecodeString(*systemPromptB64)
			if err != nil {
				fmt.Printf("Error decoding system prompt: %v\n", err)
				os.Exit(1)
			}
			finalSystemPrompt = string(decoded)
		}

		if *userPromptB64 != "" {
			decoded, err := base64.StdEncoding.DecodeString(*userPromptB64)
			if err != nil {
				fmt.Printf("Error decoding user prompt: %v\n", err)
				os.Exit(1)
			}
			finalUserPrompt = string(decoded)
		}

		if finalSystemPrompt == "" || finalUserPrompt == "" {
			fmt.Printf("Error: system-prompt and user-prompt are required for submit action\n")
			os.Exit(1)
		}

		fmt.Printf("Submitting task to executor: %s\n", executorURL)
		fmt.Printf("Code server: %s\n", *codeServerName)
		fmt.Printf("LLM config: %s\n", *llmConfigName)

		// 提交任务
		task := Task{
			ID:             *id,
			SystemPrompt:   finalSystemPrompt,
			UserPrompt:     finalUserPrompt,
			CodeServerName: *codeServerName,
			LLMConfigName:  *llmConfigName,
		}

		// 提交任务
		resp, err := publisher.SubmitTask(task)
		if err != nil {
			fmt.Printf("Error submitting task: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("\nTask submitted successfully!\n")
		fmt.Printf("Task ID: %s\n", resp.TaskID)
		fmt.Printf("Status: %s\n", resp.Status)

	case "get_sym":
		if len(os.Args) < 3 {
			fmt.Printf("Usage: task_publisher get_sym [symbol_name] --code-server name\n")
			os.Exit(1)
		}

		// 解析get_sym命令的参数
		flagSet := flag.NewFlagSet("get_sym", flag.ExitOnError)
		codeServerName := flagSet.String("code-server", "default", "Code server name")

		// 解析参数，跳过前两个参数（程序名和子命令），第三个参数是symbol_name
		flagSet.Parse(os.Args[3:])
		symbolName := os.Args[2]

		// 从executor获取配置
		config, err := publisher.GetConfig()
		if err != nil {
			fmt.Printf("Error getting config from executor: %v\n", err)
			os.Exit(1)
		}

		// 查找code server URL
		var codeServerURL string
		for _, cs := range config.CodeServers {
			if cs.Name == *codeServerName {
				codeServerURL = cs.URL
				break
			}
		}

		if codeServerURL == "" {
			fmt.Printf("Error: code server '%s' not found\n", *codeServerName)
			os.Exit(1)
		}

		// 确保URL有协议前缀
		codeServerURL = ensureURLProtocol(codeServerURL)

		// 创建code server客户端
		codeServerClient := NewCodeServerClient(codeServerURL)

		// 获取符号信息
		err = codeServerClient.GetSymbolInfo(symbolName)
		if err != nil {
			fmt.Println("Error getting symbol info: %v\n", err)
			os.Exit(1)
		}

	case "find_refs":
		if len(os.Args) < 3 {
			fmt.Printf("Usage: task_publisher find_refs [symbol_name] --code-server name\n")
			os.Exit(1)
		}

		// 解析find_refs命令的参数
		flagSet := flag.NewFlagSet("find_refs", flag.ExitOnError)
		codeServerName := flagSet.String("code-server", "default", "Code server name")

		// 解析参数，跳过前两个参数（程序名和子命令），第三个参数是symbol_name
		flagSet.Parse(os.Args[3:])
		symbolName := os.Args[2]

		// 从executor获取配置
		config, err := publisher.GetConfig()
		if err != nil {
			fmt.Printf("Error getting config from executor: %v\n", err)
			os.Exit(1)
		}

		// 查找code server URL
		var codeServerURL string
		for _, cs := range config.CodeServers {
			if cs.Name == *codeServerName {
				codeServerURL = cs.URL
				break
			}
		}

		if codeServerURL == "" {
			fmt.Printf("Error: code server '%s' not found\n", *codeServerName)
			os.Exit(1)
		}

		// 确保URL有协议前缀
		codeServerURL = ensureURLProtocol(codeServerURL)

		// 创建code server客户端
		codeServerClient := NewCodeServerClient(codeServerURL)

		// 获取所有引用
		err = codeServerClient.FindAllRefs(symbolName)
		if err != nil {
			fmt.Printf("Error finding refs: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Printf("Error: unknown subcommand '%s'\n", subcommand)
		fmt.Printf("Available subcommands: list, submit, get_sym, find_refs\n")
		os.Exit(1)
	}
}
