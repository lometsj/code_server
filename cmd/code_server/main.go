package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lometsj/code_server/static_binary/linux"
)

type SymbolInfo struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Line    int    `json:"line"`
	End     int    `json:"end"`
	Content string `json:"content"`
	File    string `json:"file"`
	Typeref string `json:"typeref,omitempty"`
}

type SymbolResponse struct {
	Status  string       `json:"status"`
	ResList []SymbolInfo `json:"res_list,omitempty"`
	Error   string       `json:"error,omitempty"`
}

type RefResponse struct {
	Callers []string `json:"callers"`
	Error   string   `json:"error,omitempty"`
}

type CodeAnalyzer struct {
	codeDir   string
	dataDir   string
	binaryDir string
}

func NewCodeAnalyzer(codeDir, dataDir string) *CodeAnalyzer {
	codeDirAbs, _ := filepath.Abs(codeDir)
	println(codeDirAbs)
	dataDirAbs, _ := filepath.Abs(dataDir)

	// 创建临时目录存放二进制文件
	tempDir, err := os.MkdirTemp("", "code-server-binaries-")
	if err != nil {
		log.Fatalf("Failed to create temp directory: %v", err)
	}

	// 提取二进制文件
	binaries := []string{"ctags", "readtags", "global", "gtags"}
	for _, binary := range binaries {
		err := extractBinary(binary, tempDir)
		if err != nil {
			log.Fatalf("Failed to extract %s: %v", binary, err)
		}
	}

	return &CodeAnalyzer{
		codeDir:   codeDirAbs,
		dataDir:   dataDirAbs,
		binaryDir: tempDir,
	}
}

func extractBinary(name, destDir string) error {
	// 从embed FS中读取二进制文件
	data, err := linux.StaticBinaries.ReadFile(name)
	if err != nil {
		return fmt.Errorf("failed to read embedded binary %s: %v", name, err)
	}

	// 创建目标文件
	destPath := filepath.Join(destDir, name)
	if err := os.WriteFile(destPath, data, 0755); err != nil {
		return fmt.Errorf("failed to write binary %s: %v", name, err)
	}

	return nil
}

func (ca *CodeAnalyzer) getBinaryPath(name string) string {
	return filepath.Join(ca.binaryDir, name)
}

func (ca *CodeAnalyzer) getCodeContent(file string, line, end int) (string, error) {
	filePath := filepath.Join(ca.codeDir, file)
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %v", filePath, err)
	}

	lines := strings.Split(string(content), "\n")
	if line < 1 || line > len(lines) || end < line || end > len(lines) {
		return "", fmt.Errorf("invalid line range %d-%d for file %s", line, end, file)
	}

	return strings.Join(lines[line-1:end], "\n"), nil
}

func (ca *CodeAnalyzer) getRefCalleeContent(filePath string, lineNum int) (string, error) {
	cmd := exec.Command(ca.getBinaryPath("ctags"), "--fields=+ne-P", "--output-format=json", "-o", "-", filePath)
	cmd.Dir = ca.codeDir
	output, err := cmd.Output()
	println(string(output))
	if err != nil {
		return "", fmt.Errorf("ctags command failed: %v", err)
	}

	syms := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, sym := range syms {
		if sym == "" {
			continue
		}

		var symDict map[string]interface{}
		if err := json.Unmarshal([]byte(sym), &symDict); err != nil {
			continue
		}

		if kind, ok := symDict["kind"].(string); !ok || kind != "function" {
			continue
		}

		symLine := int(symDict["line"].(float64))
		symEnd := int(symDict["end"].(float64))

		if lineNum > symLine && symEnd > lineNum {
			return ca.getCodeContent(filePath, symLine, symEnd)
		}
	}

	return "", nil
}

func (ca *CodeAnalyzer) GetSymbolInfo(symbol string) SymbolResponse {
	response := SymbolResponse{Status: "failed"}

	// 处理符号名称
	if strings.HasPrefix(symbol, "struct") {
		parts := strings.Fields(symbol)
		if len(parts) > 1 {
			symbol = parts[1]
		}
	}
	if strings.Contains(symbol, "->") {
		parts := strings.Fields(symbol)
		if len(parts) > 1 {
			symbol = parts[1]
		}
	}

	// 使用readtags查找符号
	cmd := exec.Command(ca.getBinaryPath("readtags"), "-t", ".tsj/tags", symbol)
	cmd.Dir = ca.codeDir
	output, err := cmd.Output()
	if err != nil {
		response.Error = fmt.Sprintf("readtags command failed: %v", err)
		return response
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		response.Error = "symbol not found"
		return response
	}
	// println(len(lines))
	println(string(output))

	var resList []SymbolInfo
	for _, line := range lines {
		println("line start")
		parts := strings.Fields(line)
		if len(parts) < 2 {
			println(line + "parse failed")
			continue
		}

		file := parts[1]
		println(parts[0])
		println(parts[1])
		println(parts[2])

		// 使用ctags获取详细信息
		ctagsCmd := exec.Command(ca.getBinaryPath("ctags"), "--fields=+ne-P", "--output-format=json", "-o", "-", file)
		println(ctagsCmd.String())
		ctagsCmd.Dir = ca.codeDir
		ctagsOutput, err := ctagsCmd.Output()
		println(string(ctagsOutput))
		if err != nil {
			continue
		}

		syms := strings.Split(strings.TrimSpace(string(ctagsOutput)), "\n")
		tmpSymToFind := symbol
		i := 0
		loopCount := 0
		maxLoops := len(syms) * 2

		for i < len(syms) && loopCount < maxLoops {
			loopCount++

			var symDict map[string]interface{}
			if err := json.Unmarshal([]byte(syms[i]), &symDict); err != nil {
				i++
				continue
			}

			if symDict["name"] != tmpSymToFind {
				i++
				continue
			}

			// 处理typeref情况
			if _, hasEnd := symDict["end"]; !hasEnd {
				if typeref, hasTyperef := symDict["typeref"].(string); hasTyperef {
					parts := strings.Split(typeref, ":")
					if len(parts) > 1 {
						tmpSymToFind = parts[1]
						i = 0
						continue
					}
				}
			}

			// 获取代码内容
			content, err := ca.getCodeContent(file, int(symDict["line"].(float64)), int(symDict["end"].(float64)))
			if err != nil {
				i++
				continue
			}

			symInfo := SymbolInfo{
				Name:    symDict["name"].(string),
				Kind:    symDict["kind"].(string),
				Line:    int(symDict["line"].(float64)),
				End:     int(symDict["end"].(float64)),
				Content: content,
				File:    file,
			}

			if typeref, ok := symDict["typeref"].(string); ok {
				symInfo.Typeref = typeref
			}

			resList = append(resList, symInfo)
			break
		}
	}

	response.Status = "success"
	response.ResList = resList
	return response
}

func (ca *CodeAnalyzer) FindAllRefs(symbol string) RefResponse {
	response := RefResponse{}

	cmd := exec.Command(ca.getBinaryPath("global"), "-xsr", symbol)
	cmd.Dir = ca.codeDir
	//GTAGSROOT要为绝对路径
	cmd.Env = append(os.Environ(), "GTAGSROOT="+ca.codeDir)
	cmd.Env = append(cmd.Env, "GTAGSDBPATH="+ca.codeDir+"/.tsj")
	output, err := cmd.Output()
	println(string(output))
	if err != nil {
		response.Error = fmt.Sprintf("global command failed: %v", err)
		return response
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		println("lines is empty")
		return response
	}

	var callersContent []string
	seen := make(map[string]bool)

	for _, line := range lines {
		println(line)
		if strings.TrimSpace(line) == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}

		// 移除未使用的变量
		// callee := parts[0]
		filePath := parts[2]
		lineNumStr := parts[1]
		// callLine := parts[3]

		lineNum, err := strconv.Atoi(lineNumStr)
		if err != nil {
			continue
		}
		println("获取文件 " + filePath + " 行号 " + lineNumStr)
		callerContent, err := ca.getRefCalleeContent(filePath, lineNum)
		if err != nil {
			continue
		}

		if callerContent != "" && !seen[callerContent] {
			callersContent = append(callersContent, callerContent)
			seen[callerContent] = true
		}
	}

	response.Callers = callersContent
	return response
}

type Server struct {
	analyzer *CodeAnalyzer
}

func (s *Server) getSymbolHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Symbol string `json:"symbol"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	response := s.analyzer.GetSymbolInfo(req.Symbol)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) findRefsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Symbol string `json:"symbol"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	response := s.analyzer.FindAllRefs(req.Symbol)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	// 解析命令行参数
	codeDir := flag.String("code-dir", ".", "代码目录路径")
	listenAddr := flag.String("listen", "0.0.0.0:0", "监听地址和端口 (格式: host:port)")

	flag.Parse()

	// 检查.tsj目录是否存在，目录下是否有tags GPATH GTAGS GRTAGS文件
	if _, err := os.Stat(".tsj"); os.IsNotExist(err) {
		log.Fatalf(".tsj目录不存在，请先运行gtags生成tags文件")
	}
	if _, err := os.Stat(".tsj/tags"); os.IsNotExist(err) {
		log.Fatalf(".tsj/tags文件不存在，请先运行ctags生成tags文件")
	}
	if _, err := os.Stat(".tsj/GPATH"); os.IsNotExist(err) {
		log.Fatalf(".tsj/GPATH文件不存在，请先运行gtags生成tags文件")
	}
	if _, err := os.Stat(".tsj/GTAGS"); os.IsNotExist(err) {
		log.Fatalf(".tsj/GTAGS文件不存在，请先运行gtags生成tags文件")
	}
	if _, err := os.Stat(".tsj/GRTAGS"); os.IsNotExist(err) {
		log.Fatalf(".tsj/GRTAGS文件不存在，请先运行gtags生成tags文件")
	}

	// 如果端口为0，让系统自动分配端口
	if strings.HasSuffix(*listenAddr, ":0") {
		listener, err := net.Listen("tcp", *listenAddr)
		if err != nil {
			log.Fatalf("Failed to listen: %v", err)
		}
		*listenAddr = listener.Addr().String()
		listener.Close()
	}

	// 创建代码分析器
	analyzer := NewCodeAnalyzer(*codeDir, "")

	// 程序退出时清理临时目录
	defer func() {
		if analyzer.binaryDir != "" {
			os.RemoveAll(analyzer.binaryDir)
		}
	}()

	// 创建HTTP服务器
	server := &Server{analyzer: analyzer}

	// 设置路由
	http.HandleFunc("/api/get_symbol", server.getSymbolHandler)
	http.HandleFunc("/api/find_refs", server.findRefsHandler)

	log.Printf("Starting server on %s", *listenAddr)
	log.Printf("Code directory: %s", *codeDir)
	log.Printf("API endpoints:")
	log.Printf("  POST /api/get_symbol - 获取符号信息")
	log.Printf("  POST /api/find_refs - 获取符号引用")

	if err := http.ListenAndServe(*listenAddr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
