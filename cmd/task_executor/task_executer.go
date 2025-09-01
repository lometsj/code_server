package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DataStore struct {
	data     Config
	mu       sync.Mutex
	filepath string
}

var dataStore = &DataStore{}

// TaskList 任务列表
var TaskList = []Task{}
var taskListMutex sync.Mutex

// TaskResult 任务结果
type TaskResult struct {
	ID        string                 `json:"id"`
	Result    map[string]interface{} `json:"result"`
	CreatedAt time.Time              `json:"created_at"`
}

// 结果目录（相对于程序所在目录）
var resultDir = "results"

// prompts目录（相对于程序所在目录）
var promptDir = "prompts"

// 获取程序所在目录
func getExecutableDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exePath)
}

// 获取结果目录的完整路径
func getResultDir() string {
	return filepath.Join(getExecutableDir(), resultDir)
}

// 获取prompts目录的完整路径
func getPromptDir() string {
	return filepath.Join(getExecutableDir(), promptDir)
}

// getConfigHandler 获取当前配置的 HTTP 处理函数
type CodeServer struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

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

// LLMConfigs 定义存储多个LLM配置的结构
type LLMConfigs struct {
	Configs []NamedLLMConfig `json:"configs"`
}

// Task 定义任务结构
type Task struct {
	ID             string `json:"id"`
	SystemPrompt   string `json:"system_prompt"`
	UserPrompt     string `json:"user_prompt"`
	CodeServerName string `json:"code_server_name"`
	LLMConfigName  string `json:"llm_config_name"`
}

// CodeAnalyzer 代码分析器
type CodeAnalyzer struct {
	ServerIP   string
	ServerPort int
	ServerURL  string
}

// NewCodeAnalyzer 创建新的代码分析器
func NewCodeAnalyzer(server string) *CodeAnalyzer {
	parts := strings.Split(server, ":")
	if len(parts) != 2 {
		// 处理错误情况
		return nil
	}

	ip := parts[0]
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		// 处理端口号转换错误
		return nil
	}

	println(ip, port)
	return &CodeAnalyzer{
		ServerIP:   ip,
		ServerPort: port,
		ServerURL:  fmt.Sprintf("http://%s:%d", ip, port),
	}
}

// GetSymbolInfo 获取符号信息
func (ca *CodeAnalyzer) GetSymbolInfo(symbol string) (string, error) {
	url := fmt.Sprintf("%s/api/get_symbol", ca.ServerURL)
	data := map[string]string{"symbol": symbol}
	json_data, _ := json.Marshal(data)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(json_data))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return string(body), nil
}

// FindAllRefs 查找所有引用
func (ca *CodeAnalyzer) FindAllRefs(symbol string) (string, error) {
	url := fmt.Sprintf("%s/api/find_refs", ca.ServerURL)
	data := map[string]string{"symbol": symbol}
	json_data, _ := json.Marshal(data)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(json_data))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	return string(body), nil
}

// LLMAnalyzer LLM分析器
type LLMAnalyzer struct {
	APIKey  string
	BaseURL string
	Model   string
}

// NewLLMAnalyzer 创建新的LLM分析器
func NewLLMAnalyzer(config *NamedLLMConfig) *LLMAnalyzer {
	return &LLMAnalyzer{
		APIKey:  config.APIKey,
		BaseURL: config.BaseURL,
		Model:   config.Model,
	}
}

// Message 消息结构
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// QueryOpenAI 调用OpenAI API进行查询
func (la *LLMAnalyzer) QueryOpenAI(messages []Message) (string, error) {
	// 添加重试机制
	maxRetries := 3
	retryDelay := 2 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		url := fmt.Sprintf("%s/chat/completions", la.BaseURL)
		data := map[string]interface{}{
			"model":             la.Model,
			"messages":          messages,
			"temperature":       0.1,
			"max_tokens":        2000,
			"top_p":             0.95,
			"frequency_penalty": 0,
			"presence_penalty":  0,
			"response_format":   map[string]string{"type": "json_object"},
		}
		json_data, _ := json.Marshal(data)

		req, _ := http.NewRequest("POST", url, bytes.NewBuffer(json_data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+la.APIKey)

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			if attempt < maxRetries-1 {
				log.Printf("API调用失败，尝试重试 (%d/%d): %v", attempt+1, maxRetries, err)
				time.Sleep(retryDelay * time.Duration(2^attempt)) // 指数退避
				continue
			} else {
				return "", err
			}
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var result map[string]interface{}
		json.Unmarshal(body, &result)

		if choices, ok := result["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if message, ok := choice["message"].(map[string]interface{}); ok {
					if content, ok := message["content"].(string); ok {
						return content, nil
					}
				}
			}
		}
		return "", fmt.Errorf("无法解析API响应，响应体: %s", body)
	}
	return "", fmt.Errorf("API调用失败")
}

// AnalyzeTask 分析任务
func (la *LLMAnalyzer) AnalyzeTask(codeAnalyzer *CodeAnalyzer, problemPrompt map[string]string) (map[string]interface{}, error) {
	messages := []Message{
		{Role: "system", Content: problemPrompt["system"] + "\n请使用工具调用获取代码信息并分析问题。"},
		{Role: "user", Content: problemPrompt["init_user"] + `\n\n【代码分析功能说明】\n你可以使用get_symbol功能获取符号定义信息，可以使用find_refs获取函数引用信息以便于向上追踪函数调用栈。\n\n【强制输出结果要求】\n必须在回答中tag字段，值为[tsj_have][tsj_nothave][tsj_next]:\n- 如判断有代码问题: [tsj_have] 并提供 {\"problem_type\": \"问题类型\", \"context\": \"代码上下文\"}\n- 如判断无代码问题: [tsj_nothave]\n- 如果不能判断，需要获取信息进一步分析，请包含[tsj_next]，并包含get_symbol或者find_refs请求获取更多代码信息,详细格式如下：\n1. 如果需要知道某个函数，宏或者变量的定义，使用get_symbol获取符号信息: {\"command\": \"get_symbol\", \"sym_name\": \"符号名称\"}\n2. 如果需要进一步分析数据流，使用find_refs获取调用信息: {\"command\": \"find_refs\", \"sym_name\": \"符号名称\"}\n\n【输出要求】\n【JSON格式返回要求】\n请以JSON格式返回你的回答，例如：\n{\"tag\": \"tsj_have\", \"problem_info\": {\"problem_type\": \"问题类型\", \"context\": \"代码上下文\"}, \"response\": \"你的分析和解释\"}\n或\n{\"tag\": \"tsj_nothave\", \"response\": \"你的分析和解释\"}\n或\n{\"tag\": \"tsj_next\", \"requests\": [{\"command\": \"get_symbol\", \"sym_name\": \"符号名称\"}], \"response\": \"你的分析和解释\"}\n或\n{\"tag\": \"tsj_next\", \"requests\": [{\"command\": \"find_refs\", \"sym_name\": \"符号名称\"}], \"response\": \"你的分析和解释\"}\n或\n{\"tag\": \"tsj_next\", \"requests\": [{\"command\": \"get_symbol\", \"sym_name\": \"符号名称\"},{\"command\": \"find_refs\", \"sym_name\": \"符号名称\"},{\"command\": \"find_refs\", \"sym_name\": \"符号名称\"}], \"response\": \"你的分析和解释\"}`},
	}

	conversationComplete := false
	maxTurns := 5
	turn := 0

	result := map[string]interface{}{
		"has_problem_info": false,
		"problem_info":     nil,
		"conversation":     []Message{},
	}

	for !conversationComplete && turn < maxTurns {
		// 调用OpenAI API获取响应
		llmResponse, err := la.QueryOpenAI(messages)
		if err != nil {
			return nil, err
		}

		// 处理普通响应
		messages = append(messages, Message{Role: "assistant", Content: llmResponse})

		var message map[string]interface{}
		json.Unmarshal([]byte(llmResponse), &message)
		fmt.Printf("LLM Response: %+v\n", message)

		// 检查是否包含问题信息,通过tag判断，如果是tsj_have或者tsj_nothave就结束对话并将结果保存
		if tag, ok := message["tag"].(string); ok {
			switch tag {
			case "tsj_have", "tsj_nothave":
				conversationComplete = true
				result["has_problem_info"] = (tag == "tsj_have")
				result["problem_info"] = message["problem_info"]
				result["response"] = message["response"]
			case "tsj_next":
				// 处理tsj_next标签，添加请求到消息列表
				if requests, ok := message["requests"].([]any); ok {
					for _, req := range requests {
						if request, ok := req.(map[string]any); ok {
							if command, ok := request["command"].(string); ok {
								if symName, ok := request["sym_name"].(string); ok {
									switch command {
									case "get_symbol":
										info, err := codeAnalyzer.GetSymbolInfo(symName)
										if err != nil {
											//todo
											return nil, err
										}
										messages = append(messages, Message{Role: "user", Content: info})
									case "find_refs":
										refs, err := codeAnalyzer.FindAllRefs(symName)
										if err != nil {
											//todo
											return nil, err
										}
										messages = append(messages, Message{Role: "user", Content: refs})
									}
								}
							}
						}
					}
				}
			}
		}
		turn++
	}

	if turn == maxTurns && !conversationComplete {
		result["has_problem_info"] = true
		result["problem_info"] = "对话轮数耗尽仍没有问答，建议重点审视。"
	}

	result["conversation"] = messages
	return result, nil
}

// TaskQueue 任务队列
var TaskQueue = make(chan Task, 2000)

// generateTaskID 生成任务ID
func generateTaskID() string {
	return fmt.Sprintf("task_%d", time.Now().Unix())
}

// saveTaskResult 保存任务结果
func saveTaskResult(taskID string, result map[string]interface{}) error {
	// 确保results目录存在
	resultPath := getResultDir()
	if err := os.MkdirAll(resultPath, 0755); err != nil {
		return err
	}

	var results []map[string]interface{}

	// 检查是否已有该ID的结果文件
	filePath := filepath.Join(resultPath, taskID+".json")
	if _, err := os.Stat(filePath); err == nil {
		// 文件存在，读取现有结果
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &results); err != nil {
			return err
		}

	} else {
		// 文件不存在，创建新文件
		results = []map[string]interface{}{}
	}

	results = append(results, result)

	// 保存到文件
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

// executeTask 执行任务的函数
func executeTask(task Task) {
	fmt.Printf("Executing task: %+v\n", task)

	// 获取code server配置
	var codeServerURL string

	// 查找指定的code server配置
	for _, cs := range dataStore.data.CodeServers {
		if cs.Name == task.CodeServerName {
			codeServerURL = cs.URL
			break
		}
	}

	// 如果没有配置可用，记录错误并返回
	if codeServerURL == "" {
		fmt.Printf("No code server configuration available for task\n")
		return
	}

	// 初始化代码分析器
	codeAnalyzer := NewCodeAnalyzer(codeServerURL)
	if codeAnalyzer == nil {
		fmt.Printf("Error initializing code analyzer\ncheck code server url: %s", codeServerURL)
		return
	}

	// 查找指定的LLM配置
	var selectedConfig *NamedLLMConfig
	for _, config := range dataStore.data.LLMConfigs {
		if config.Name == task.LLMConfigName {
			selectedConfig = &config
			break
		}
	}

	// 如果没有配置可用，记录错误并返回
	if selectedConfig == nil {
		fmt.Printf("No LLM configuration available for task\n")
		return
	}

	// 初始化LLM分析器
	llmAnalyzer := NewLLMAnalyzer(selectedConfig)

	// 准备问题上下文
	problemPrompt := map[string]string{
		"system":    task.SystemPrompt,
		"init_user": task.UserPrompt,
	}

	// 分析任务
	result, err := llmAnalyzer.AnalyzeTask(codeAnalyzer, problemPrompt)
	if err != nil {
		fmt.Printf("Error analyzing task: %v\n", err)
		return
	}

	// 保存任务结果
	if err := saveTaskResult(task.ID, result); err != nil {
		fmt.Printf("Error saving task result: %v\n", err)
		return
	}

	// 输出结果
	fmt.Printf("Task result: %+v\n", result)
}

// taskWorker 任务工作协程
func taskWorker() {
	for task := range TaskQueue {
		executeTask(task)
		// 任务执行完成后，从任务列表中移除
		taskListMutex.Lock()
		for i, t := range TaskList {
			if t.ID == task.ID {
				TaskList = append(TaskList[:i], TaskList[i+1:]...)
				break
			}
		}
		taskListMutex.Unlock()
	}
}

// submitTaskHandler 接收任务的 HTTP 处理函数
func submitTaskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var task Task
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	// 如果没有提供ID，生成一个
	if task.ID == "" {
		task.ID = generateTaskID()
	}

	// 添加到任务列表
	taskListMutex.Lock()
	TaskList = append(TaskList, task)
	taskListMutex.Unlock()

	// 将任务添加到队列
	TaskQueue <- task

	// 返回响应
	response := map[string]interface{}{
		"status":  "success",
		"message": "Task received",
		"task_id": task.ID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// BatchTaskRequest 批量任务请求结构
type BatchTaskRequest struct {
	ProblemType string   `json:"problem_type"`
	ID          string   `json:"id"`
	Functions   []string `json:"function"`
	LLMConfig   string   `json:"llm_config"`
	CodeServer  string   `json:"code_server"`
}

// PromptTemplate prompt模板结构
type PromptTemplate struct {
	System   string `json:"system"`
	InitUser string `json:"init_user"`
}

// loadPromptTemplate 从prompt文件夹加载prompt模板
func loadPromptTemplate(problemType string) (*PromptTemplate, error) {
	promptPath := filepath.Join(getPromptDir(), problemType+".json")
	data, err := os.ReadFile(promptPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read prompt template for %s: %v", problemType, err)
	}

	var template PromptTemplate
	if err := json.Unmarshal(data, &template); err != nil {
		return nil, fmt.Errorf("failed to unmarshal prompt template: %v", err)
	}

	return &template, nil
}

// renderPrompt 渲染prompt模板
func renderPrompt(template *PromptTemplate, functionName, functionContent string) map[string]string {
	systemPrompt := strings.ReplaceAll(template.System, "{function_name}", functionName)
	userPrompt := strings.ReplaceAll(template.InitUser, "{function_name}", functionName)
	userPrompt = strings.ReplaceAll(userPrompt, "{function_content}", functionContent)

	return map[string]string{
		"system":    systemPrompt,
		"init_user": userPrompt,
	}
}

// submitBatchTaskHandler 批量提交任务的 HTTP 处理函数
func submitBatchTaskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var request BatchTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	// 验证必要参数
	if request.ProblemType == "" || len(request.Functions) == 0 || request.LLMConfig == "" || request.CodeServer == "" {
		http.Error(w, "Missing required parameters", http.StatusBadRequest)
		return
	}

	// 加载prompt模板
	promptTemplate, err := loadPromptTemplate(request.ProblemType)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load prompt template: %v", err), http.StatusInternalServerError)
		return
	}

	// 获取code server配置
	var codeServerURL string
	for _, cs := range dataStore.data.CodeServers {
		if cs.Name == request.CodeServer {
			codeServerURL = cs.URL
			break
		}
	}

	if codeServerURL == "" {
		http.Error(w, "Code server not found", http.StatusBadRequest)
		return
	}

	// 初始化代码分析器
	codeAnalyzer := NewCodeAnalyzer(codeServerURL)
	if codeAnalyzer == nil {
		http.Error(w, "Failed to initialize code analyzer", http.StatusInternalServerError)
		return
	}

	// 为每个function创建任务
	var taskIDs []string
	for _, functionName := range request.Functions {
		// 查找function的调用点
		refs, err := codeAnalyzer.FindAllRefs(functionName)
		if err != nil {
			fmt.Printf("Failed to find refs for %s: %v\n", functionName, err)
			continue
		}

		// 解析JSON响应，获取callers列表
		var refsData map[string]interface{}
		if err := json.Unmarshal([]byte(refs), &refsData); err != nil {
			fmt.Printf("Failed to parse refs JSON for %s: %v\n", functionName, err)
			continue
		}

		// 获取callers数组
		callers, ok := refsData["callers"].([]interface{})
		if !ok {
			fmt.Printf("No callers found for %s\n", functionName)
			continue
		}

		// 为每个caller创建任务
		for _, caller := range callers {
			callerStr, ok := caller.(string)
			if !ok || strings.TrimSpace(callerStr) == "" {
				continue
			}

			// 渲染prompt
			prompt := renderPrompt(promptTemplate, functionName, callerStr)

			// 创建任务
			task := Task{
				ID:             request.ID,
				SystemPrompt:   prompt["system"],
				UserPrompt:     prompt["init_user"],
				CodeServerName: request.CodeServer,
				LLMConfigName:  request.LLMConfig,
			}

			// 添加到任务列表和队列
			taskListMutex.Lock()
			TaskList = append(TaskList, task)
			taskListMutex.Unlock()

			TaskQueue <- task
			taskIDs = append(taskIDs, task.ID)
		}
	}

	// 返回响应
	response := map[string]interface{}{
		"status":   "success",
		"message":  "Batch tasks submitted",
		"task_ids": taskIDs,
		"count":    len(taskIDs),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// getTaskStatusHandler 获取任务状态的 HTTP 处理函数
func getTaskStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.URL.Query().Get("id")
	if taskID == "" {
		http.Error(w, "Task ID is required", http.StatusBadRequest)
		return
	}

	// 遍历任务列表，查找是否有同名task
	taskListMutex.Lock()
	defer taskListMutex.Unlock()

	found := false
	for _, task := range TaskList {
		if task.ID == taskID {
			found = true
			break
		}
	}

	response := map[string]interface{}{
		"exists": found,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// getResultListHandler 获取结果列表的 HTTP 处理函数
func getResultListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 确保results目录存在
	if err := os.MkdirAll(resultDir, 0755); err != nil {
		http.Error(w, "Failed to create results directory", http.StatusInternalServerError)
		return
	}

	// 读取results目录下的所有文件
	files, err := os.ReadDir(resultDir)
	if err != nil {
		http.Error(w, "Failed to read results directory", http.StatusInternalServerError)
		return
	}

	var resultFiles []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			resultFiles = append(resultFiles, file.Name())
		}
	}

	response := map[string]interface{}{
		"results": resultFiles,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// exportResultHandler 导出结果的 HTTP 处理函数
func exportResultHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	fileName := r.URL.Query().Get("file")
	if fileName == "" {
		http.Error(w, "File name is required", http.StatusBadRequest)
		return
	}

	// 安全检查：确保文件名不包含路径遍历字符
	if strings.Contains(fileName, "..") || strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") {
		http.Error(w, "Invalid file name", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(resultDir, fileName)

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// 读取文件内容
	data, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}

	// 设置响应头
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename="+fileName)
	w.Write(data)
}

// deleteResultHandler 删除结果的 HTTP 处理函数
func deleteResultHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	fileName := r.URL.Query().Get("file")
	if fileName == "" {
		http.Error(w, "File name is required", http.StatusBadRequest)
		return
	}

	// 安全检查：确保文件名不包含路径遍历字符
	if strings.Contains(fileName, "..") || strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") {
		http.Error(w, "Invalid file name", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(resultDir, fileName)

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// 删除文件
	if err := os.Remove(filePath); err != nil {
		http.Error(w, "Failed to delete file", http.StatusInternalServerError)
		return
	}

	response := map[string]string{
		"status":  "success",
		"message": "File deleted successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// getConfigPath 获取配置文件路径
func getConfigPath(configPath string) string {
	// 如果命令行参数指定了配置文件路径，则使用该路径
	if configPath != "" {
		return configPath
	}

	// 否则，获取可执行文件的路径
	execPath, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}

	// 获取可执行文件所在的目录
	execDir := filepath.Dir(execPath)

	// 返回默认配置文件路径
	return filepath.Join(execDir, "config.json")
}

// configPageHandler 配置页面的 HTTP 处理函数
func configPageHandler(w http.ResponseWriter, r *http.Request) {
	// 读取配置页面的 HTML 文件
	configPath := getConfigPath("")
	execDir := filepath.Dir(configPath)
	htmlPath := filepath.Join(execDir, "config.html")

	// 读取HTML文件内容
	htmlContent, err := os.ReadFile(htmlPath)
	if err != nil {
		// 如果读取失败，返回错误
		http.Error(w, "Failed to read config page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(htmlContent)
}

func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	dataStore.mu.Lock()
	defer dataStore.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dataStore.data)
}

func (ds *DataStore) saveFullConfig() error {
	configData, err := json.MarshalIndent(ds.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ds.filepath, configData, 0644)
}

func handleUpdateLLM(w http.ResponseWriter, r *http.Request) {
	dataStore.mu.Lock()
	defer dataStore.mu.Unlock()
	var config NamedLLMConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, `{"error":"无效请求格式"}`, http.StatusBadRequest)
		return
	}

	//如果有相同name就更新，没有就新增
	var found bool
	found = false
	for i, cfg := range dataStore.data.LLMConfigs {
		if cfg.Name == config.Name {
			dataStore.data.LLMConfigs[i] = config
			found = true
			break
		}
	}
	if !found {
		dataStore.data.LLMConfigs = append(dataStore.data.LLMConfigs, config)
	}
	if err := dataStore.saveFullConfig(); err != nil {
		http.Error(w, `{"error":"配置保存失败"}`, http.StatusInternalServerError)
		return
	}
}

func handleUpdateCodeServer(w http.ResponseWriter, r *http.Request) {
	dataStore.mu.Lock()
	defer dataStore.mu.Unlock()
	var config CodeServer
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, `{"error":"无效请求格式"}`, http.StatusBadRequest)
		return
	}

	//如果有相同name就更新，没有就新增
	var found bool
	found = false
	for i, cfg := range dataStore.data.CodeServers {
		if cfg.Name == config.Name {
			dataStore.data.CodeServers[i] = config
			found = true
			break
		}
	}
	if !found {
		dataStore.data.CodeServers = append(dataStore.data.CodeServers, config)
	}
	if err := dataStore.saveFullConfig(); err != nil {
		http.Error(w, `{"error":"配置保存失败"}`, http.StatusInternalServerError)
		return
	}
}

func handleDeleteConfig(w http.ResponseWriter, r *http.Request) {
	dataStore.mu.Lock()
	defer dataStore.mu.Unlock()
	var deleteConfig struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&deleteConfig); err != nil {
		http.Error(w, `{"error":"无效请求格式"}`, http.StatusBadRequest)
		return
	}

	var found bool
	found = false
	// 根据type选择遍历哪个config并检查name相同的就删除
	if deleteConfig.Type == "llm" {
		for i, cfg := range dataStore.data.LLMConfigs {
			if cfg.Name == deleteConfig.Name {
				found = true
				// 如果是默认llm，需要更新默认llm
				dataStore.data.LLMConfigs = append(dataStore.data.LLMConfigs[:i], dataStore.data.LLMConfigs[i+1:]...)
				break
			}
		}
	} else if deleteConfig.Type == "code_server" {
		for i, cfg := range dataStore.data.CodeServers {
			if cfg.Name == deleteConfig.Name {
				found = true
				dataStore.data.CodeServers = append(dataStore.data.CodeServers[:i], dataStore.data.CodeServers[i+1:]...)
				break
			}
		}
	} else {
		http.Error(w, `{"error":"无效的配置类型"}`, http.StatusBadRequest)
		return
	}
	if !found {
		http.Error(w, `{"error":"没有找到对应的配置"}`, http.StatusBadRequest)
		return
	}

	if err := dataStore.saveFullConfig(); err != nil {
		http.Error(w, `{"error":"配置保存失败"}`, http.StatusInternalServerError)
		return
	}
}

func (ds *DataStore) LoadData() error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	dataBytes, err := os.ReadFile(ds.filepath)
	if err != nil {
		if os.IsNotExist(err) {
			//文件不存在就创建一个初始空的文件，有config结构体的结构
			initialConfig := Config{}
			// 初始化默认的llm配置
			initialConfig.LLMConfigs = append(initialConfig.LLMConfigs, NamedLLMConfig{
				Name:    "changeme",
				APIKey:  "",
				BaseURL: "",
				Model:   "",
			})
			// 初始化默认的code server配置
			initialConfig.CodeServers = append(initialConfig.CodeServers, CodeServer{
				Name: "changeme",
				URL:  "",
			})
			initialConfigData, err := json.MarshalIndent(initialConfig, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal initial config: %w", err)
			}
			if err := os.WriteFile(ds.filepath, initialConfigData, 0644); err != nil {
				return fmt.Errorf("failed to write initial config file: %w", err)
			}
			ds.data = initialConfig
			return nil
		}
		return fmt.Errorf("failed to read file: %w", err)
	}

	if err := json.Unmarshal(dataBytes, &ds.data); err != nil {
		return fmt.Errorf("failed to unmarshal data: %w", err)
	}
	return nil
}

func main() {
	// 定义命令行参数
	configPath := flag.String("config", "", "Path to the LLM config file (default: llm_config.json in the same directory as the executable)")
	port := flag.String("port", ":8080", "Port to listen on (default: :8080)")
	flag.Parse()

	// 加载配置
	dataStore.filepath = getConfigPath(*configPath)
	if err := dataStore.LoadData(); err != nil {
		log.Fatal("Failed to load configs: ", err)
	}

	// 启动任务工作协程
	go taskWorker()

	// 注册 HTTP 处理函数
	http.HandleFunc("/api/submit_task", submitTaskHandler)
	http.HandleFunc("/api/submit_batch_task", submitBatchTaskHandler)
	http.HandleFunc("/api/task_status", getTaskStatusHandler)
	http.HandleFunc("/api/task_num", getTaskNumHandler)   // 新增的任务数量接口
	http.HandleFunc("/api/task_list", getTaskListHandler) // 新增的任务列表接口
	http.HandleFunc("/api/result_list", getResultListHandler)
	http.HandleFunc("/api/export_result", exportResultHandler)
	http.HandleFunc("/api/delete_result", deleteResultHandler)
	http.HandleFunc("/api/prompt_templates", getPromptTemplatesHandler) // 新增的prompt模板列表接口
	http.HandleFunc("/api/prompt_list", getPromptListHandler)           // 新增的提示词列表接口
	http.HandleFunc("/api/update_prompt", updatePromptHandler)          // 新增的更新提示词接口
	http.HandleFunc("/api/create_prompt", createPromptHandler)          // 新增的创建提示词接口
	http.HandleFunc("/api/delete_prompt", deletePromptHandler)          // 新增的删除提示词接口
	http.HandleFunc("/config", configPageHandler)
	http.HandleFunc("/get_config", handleGetConfig)
	http.HandleFunc("/api/update_llm", handleUpdateLLM)
	http.HandleFunc("/api/update_code_server", handleUpdateCodeServer)
	http.HandleFunc("/api/delete_config", handleDeleteConfig)

	// 添加静态文件路由
	staticPath := filepath.Join(getExecutableDir(), "static")
	fs := http.FileServer(http.Dir(staticPath))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// 启动 HTTP 服务器
	fmt.Printf("Task executor server starting on port %s...\n", *port)
	fmt.Printf("Configuration page available at http://localhost%s/config\n", *port)
	log.Fatal(http.ListenAndServe(*port, nil))
}

// getTaskNumHandler 获取任务数量的 HTTP 处理函数
func getTaskNumHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 获取任务数量
	taskListMutex.Lock()
	taskCount := len(TaskList)
	taskListMutex.Unlock()

	response := map[string]interface{}{
		"task_count": taskCount,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// getPromptTemplatesHandler 获取prompt模板列表的 HTTP 处理函数
func getPromptTemplatesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 获取prompts文件夹中的模板文件列表
	promptPath := getPromptDir()
	files, err := os.ReadDir(promptPath)
	if err != nil {
		// 如果文件夹不存在，返回空列表
		response := map[string]interface{}{
			"templates": []string{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	var templates []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			// 移除.json后缀
			templateName := strings.TrimSuffix(file.Name(), ".json")
			templates = append(templates, templateName)
		}
	}

	response := map[string]interface{}{
		"templates": templates,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// PromptInfo 提示词信息结构
type PromptInfo struct {
	Name     string `json:"name"`
	System   string `json:"system"`
	InitUser string `json:"init_user"`
}

// getPromptListHandler 获取提示词列表的 HTTP 处理函数
func getPromptListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 获取prompts文件夹中的所有提示词文件
	promptPath := getPromptDir()
	files, err := os.ReadDir(promptPath)
	if err != nil {
		// 如果文件夹不存在，返回空列表
		response := map[string]interface{}{
			"prompts": []PromptInfo{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	var prompts []PromptInfo
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			// 读取提示词文件内容
			filePath := filepath.Join(promptPath, file.Name())
			data, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}

			var prompt PromptTemplate
			if err := json.Unmarshal(data, &prompt); err != nil {
				continue
			}

			// 移除.json后缀作为名称
			name := strings.TrimSuffix(file.Name(), ".json")
			prompts = append(prompts, PromptInfo{
				Name:     name,
				System:   prompt.System,
				InitUser: prompt.InitUser,
			})
		}
	}

	response := map[string]interface{}{
		"prompts": prompts,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// updatePromptHandler 更新提示词的 HTTP 处理函数
func updatePromptHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var promptInfo PromptInfo
	if err := json.NewDecoder(r.Body).Decode(&promptInfo); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	// 验证必要参数
	if promptInfo.Name == "" || promptInfo.System == "" || promptInfo.InitUser == "" {
		http.Error(w, "Missing required parameters", http.StatusBadRequest)
		return
	}

	// 确保prompts文件夹存在
	promptPath := getPromptDir()
	if err := os.MkdirAll(promptPath, 0755); err != nil {
		http.Error(w, "Failed to create prompts directory", http.StatusInternalServerError)
		return
	}

	// 创建提示词文件路径
	filePath := filepath.Join(promptPath, promptInfo.Name+".json")

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "Prompt not found", http.StatusNotFound)
		return
	}

	// 创建提示词模板
	promptTemplate := PromptTemplate{
		System:   promptInfo.System,
		InitUser: promptInfo.InitUser,
	}

	// 保存到文件
	data, err := json.MarshalIndent(promptTemplate, "", "  ")
	if err != nil {
		http.Error(w, "Failed to marshal prompt data", http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		http.Error(w, "Failed to save prompt file", http.StatusInternalServerError)
		return
	}

	response := map[string]string{
		"status":  "success",
		"message": "Prompt updated successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// createPromptHandler 创建提示词的 HTTP 处理函数
func createPromptHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var promptInfo PromptInfo
	if err := json.NewDecoder(r.Body).Decode(&promptInfo); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	// 验证必要参数
	if promptInfo.Name == "" || promptInfo.System == "" || promptInfo.InitUser == "" {
		http.Error(w, "Missing required parameters", http.StatusBadRequest)
		return
	}

	// 确保prompts文件夹存在
	promptPath := getPromptDir()
	if err := os.MkdirAll(promptPath, 0755); err != nil {
		http.Error(w, "Failed to create prompts directory", http.StatusInternalServerError)
		return
	}

	// 创建提示词文件路径
	filePath := filepath.Join(promptPath, promptInfo.Name+".json")

	// 检查文件是否已存在
	if _, err := os.Stat(filePath); err == nil {
		http.Error(w, "Prompt already exists", http.StatusConflict)
		return
	}

	// 创建提示词模板
	promptTemplate := PromptTemplate{
		System:   promptInfo.System,
		InitUser: promptInfo.InitUser,
	}

	// 保存到文件
	data, err := json.MarshalIndent(promptTemplate, "", "  ")
	if err != nil {
		http.Error(w, "Failed to marshal prompt data", http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		http.Error(w, "Failed to save prompt file", http.StatusInternalServerError)
		return
	}

	response := map[string]string{
		"status":  "success",
		"message": "Prompt created successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func deletePromptHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var deleteRequest struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&deleteRequest); err != nil {
		http.Error(w, `{"error":"无效请求格式"}`, http.StatusBadRequest)
		return
	}

	// 构建提示词文件路径
	promptPath := getPromptDir()
	promptFile := filepath.Join(promptPath, deleteRequest.Name+".json")

	// 检查文件是否存在
	if _, err := os.Stat(promptFile); os.IsNotExist(err) {
		http.Error(w, `{"error":"提示词不存在"}`, http.StatusBadRequest)
		return
	}

	// 删除提示词文件
	if err := os.Remove(promptFile); err != nil {
		http.Error(w, `{"error":"删除提示词文件失败"}`, http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"message": "提示词删除成功",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// getTaskListHandler 获取任务列表的 HTTP 处理函数，支持分页
func getTaskListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 获取分页参数
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")

	// 设置默认值
	page := 1
	limit := 10

	// 解析分页参数
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	// 计算偏移量
	offset := (page - 1) * limit

	// 获取任务列表
	taskListMutex.Lock()
	totalTasks := len(TaskList)

	// 确保偏移量不超过任务总数
	if offset >= totalTasks {
		offset = totalTasks
	}

	// 计算结束位置
	end := offset + limit
	if end > totalTasks {
		end = totalTasks
	}

	// 获取当前页的任务
	var pageTasks []Task
	if offset < totalTasks {
		pageTasks = make([]Task, end-offset)
		copy(pageTasks, TaskList[offset:end])
	}
	taskListMutex.Unlock()

	response := map[string]interface{}{
		"tasks":       pageTasks,
		"total":       totalTasks,
		"page":        page,
		"limit":       limit,
		"total_pages": (totalTasks + limit - 1) / limit,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
