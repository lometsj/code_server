package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lometsj/code_server/static_binary/linux"
)

func main() {
	// 解析命令行参数
	codeDir := flag.String("code-dir", ".", "代码目录路径")
	help := flag.Bool("h", false, "显示帮助信息")

	flag.Parse()

	if *help {
		fmt.Println("Usage: tag_init [options]")
		fmt.Println("Options:")
		fmt.Println("  -code-dir string")
		fmt.Println("    	代码目录路径 (default \".\")")
		fmt.Println("  -h	显示帮助信息")
		return
	}

	// 获取绝对路径
	codeDirAbs, err := filepath.Abs(*codeDir)
	if err != nil {
		log.Fatalf("无法获取代码目录的绝对路径: %v", err)
	}

	// 检查目录是否存在
	if _, err := os.Stat(codeDirAbs); os.IsNotExist(err) {
		log.Fatalf("代码目录不存在: %s", codeDirAbs)
	}

	// 创建临时目录存放二进制文件
	tempDir, err := os.MkdirTemp("", "tag-init-binaries-")
	if err != nil {
		log.Fatalf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(tempDir) // 程序退出时清理临时目录

	// 提取二进制文件
	binaries := []string{"ctags", "gtags"}
	for _, binary := range binaries {
		err := extractBinary(binary, tempDir)
		if err != nil {
			log.Fatalf("提取 %s 失败: %v", binary, err)
		}
	}

	// 获取二进制文件路径
	ctagsPath := filepath.Join(tempDir, "ctags")
	gtagsPath := filepath.Join(tempDir, "gtags")

	// 切换到代码目录
	err = os.Chdir(codeDirAbs)
	if err != nil {
		log.Fatalf("切换到代码目录失败: %v", err)
	}

	// 生成filelist文件，使用Go原生系统调用替代find命令
	err = generateFileList(codeDirAbs, "filelist")
	if err != nil {
		log.Fatalf("生成filelist失败: %v", err)
	}

	// 创建.tsj目录
	tsjDir := filepath.Join(codeDirAbs, ".tsj")
	if _, err := os.Stat(tsjDir); os.IsNotExist(err) {
		err = os.Mkdir(tsjDir, 0755)
		if err != nil {
			log.Fatalf("创建.tsj目录失败: %v", err)
		}
	}

	// 运行ctags生成tags文件
	fmt.Println("运行ctags生成tags文件...")
	cmd := exec.Command(ctagsPath, "-L", "filelist", "-o", filepath.Join(tsjDir, "tags"))
	cmd.Dir = codeDirAbs
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("ctags执行输出: %s", string(output))
		log.Fatalf("ctags执行失败: %v", err)
	}

	// 运行gtags生成其他tag文件
	fmt.Println("运行gtags生成其他tag文件...")
	cmd = exec.Command(gtagsPath, "-i", "-f", "filelist", "-o", tsjDir)
	cmd.Dir = codeDirAbs
	cmd.Env = append(os.Environ(), "GTAGSROOT="+codeDirAbs, "GTAGSDBPATH="+tsjDir)
	output, err = cmd.CombinedOutput()
	if err != nil {
		log.Printf("gtags执行输出: %s", string(output))
		log.Fatalf("gtags执行失败: %v", err)
	}

	fmt.Println("Tag文件生成完成!")
	fmt.Println("生成的文件:")
	fmt.Println("  .tsj/tags")
	fmt.Println("  .tsj/GPATH")
	fmt.Println("  .tsj/GTAGS")
	fmt.Println("  .tsj/GRTAGS")
}

// extractBinary 从embed FS中提取二进制文件
func extractBinary(name, destDir string) error {
	// 从embed FS中读取二进制文件
	data, err := linux.StaticBinaries.ReadFile(name)
	if err != nil {
		return fmt.Errorf("读取内嵌二进制文件 %s 失败: %v", name, err)
	}

	// 创建目标文件
	destPath := filepath.Join(destDir, name)
	if err := os.WriteFile(destPath, data, 0755); err != nil {
		return fmt.Errorf("写入二进制文件 %s 失败: %v", name, err)
	}

	return nil
}

// generateFileList 使用Go原生系统调用生成filelist文件
func generateFileList(rootDir, outputFile string) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("创建文件 %s 失败: %v", outputFile, err)
	}
	defer file.Close()

	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录本身
		if path == rootDir {
			return nil
		}

		// 跳过隐藏目录（如.git, .tsj等）
		if strings.HasPrefix(info.Name(), ".") && info.IsDir() {
			return filepath.SkipDir
		}

		// 只处理文件
		if !info.IsDir() {
			// 检查文件扩展名
			ext := filepath.Ext(info.Name())
			if ext == ".c" || ext == ".h" {
				// 计算相对于rootDir的路径
				relPath, err := filepath.Rel(rootDir, path)
				if err != nil {
					return err
				}
				_, err = file.WriteString(relPath + "\n")
				if err != nil {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("遍历目录失败: %v", err)
	}

	return nil
}