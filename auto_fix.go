package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AutoFixSystem handles automatic error detection and fixing
type AutoFixSystem struct {
	logDir      string
	maxRetries  int
	retryCount  int
	fixHistory  []string
	lastError   string
	lastFixTime time.Time
}

// NewAutoFixSystem creates a new auto-fix system
func NewAutoFixSystem() *AutoFixSystem {
	logDir := filepath.Join(".", "logs")
	os.MkdirAll(logDir, 0755)

	return &AutoFixSystem{
		logDir:      logDir,
		maxRetries:  5,
		retryCount:  0,
		fixHistory:  []string{},
		lastError:   "",
		lastFixTime: time.Now(),
	}
}

// createLogFile creates a timestamped log file
func (afs *AutoFixSystem) createLogFile(prefix string) (string, *os.File, error) {
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s.log", prefix, timestamp)
	path := filepath.Join(afs.logDir, filename)

	file, err := os.Create(path)
	if err != nil {
		return "", nil, err
	}

	return path, file, nil
}

// logMessage logs a message with timestamp
func (afs *AutoFixSystem) logMessage(category, message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	entry := fmt.Sprintf("[%s] [%s] %s\n", timestamp, category, message)

	// Write to latest log file
	logPath := filepath.Join(afs.logDir, "latest.log")
	if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		f.WriteString(entry)
		f.Close()
	}

	// Also print to console
	log.Printf("[AutoFix] %s: %s", category, message)
}

// analyzeError analyzes error and suggests fix
func (afs *AutoFixSystem) analyzeError(errorMsg string) string {
	afs.lastError = errorMsg
	afs.logMessage("ERROR", errorMsg)

	// Common error patterns and fixes
	patterns := []struct {
		pattern string
		fix     string
		action  string
	}{
		// Go build errors
		{"undefined:", "変数/関数が未定義", "go mod tidy を実行"},
		{"cannot find package", "パッケージが見つからない", "go get を実行または go.mod を確認"},
		{"imported and not used", "未使用のインポート", "未使用インポートを削除"},
		{"declared and not used", "未使用の変数", "変数を使用するか削除"},
		{"missing go.sum entry", "go.sum が古い", "go mod tidy を実行"},

		// File errors
		{"no such file or directory", "ファイル/ディレクトリが存在しない", "ファイルを作成またはパスを確認"},
		{"permission denied", "権限不足", "権限を変更または sudo を使用"},

		// Stream Deck errors
		{"Stream Deck:", "Stream Deck 接続エラー", "デバイスを再接続"},
		{"USB device", "USB デバイスエラー", "USB 接続を確認"},

		// Twitch API errors
		{"401", "認証エラー", "トークンを更新"},
		{"404", "API エンドポイントが見つからない", "URL を確認"},
		{"429", "レート制限", "待機後に再試行"},
	}

	for _, p := range patterns {
		if strings.Contains(errorMsg, p.pattern) {
			suggestion := fmt.Sprintf("検出: %s → 修正: %s → アクション: %s", p.pattern, p.fix, p.action)
			afs.logMessage("ANALYSIS", suggestion)
			return p.action
		}
	}

	// Generic fix
	genericFix := "一般的なエラー: コードを確認して構文エラーを修正"
	afs.logMessage("ANALYSIS", genericFix)
	return "コードをレビューして構文エラーを修正"
}

// executeFix executes the suggested fix
func (afs *AutoFixSystem) executeFix(fixAction string) bool {
	afs.retryCount++
	afs.fixHistory = append(afs.fixHistory, fmt.Sprintf("試行 %d: %s", afs.retryCount, fixAction))
	afs.logMessage("FIX_ATTEMPT", fmt.Sprintf("試行 %d/%d: %s", afs.retryCount, afs.maxRetries, fixAction))

	// Map fix actions to commands
	fixCommands := map[string][]string{
		"go mod tidy を実行":          {"go", "mod", "tidy"},
		"go get を実行または go.mod を確認": {"go", "get", "./..."},
		"未使用インポートを削除":              {"go", "fmt", "./..."},
		"変数を使用するか削除":               {"go", "vet", "./..."},
		"ファイルを作成またはパスを確認":          {"ls", "-la"},
		"権限を変更または sudo を使用":        {"chmod", "+x", "build.sh"},
		"コードをレビューして構文エラーを修正":       {"go", "build", "./..."},
		"トークンを更新":                  {"rm", "-f", filepath.Join(configDir, "tokens.json")},
	}

	// Execute the fix command
	if cmd, ok := fixCommands[fixAction]; ok {
		afs.logMessage("EXECUTE", strings.Join(cmd, " "))

		output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
		if err != nil {
			afs.logMessage("FIX_FAILED", fmt.Sprintf("%s: %v", string(output), err))
			return false
		}

		afs.logMessage("FIX_SUCCESS", string(output))
		return true
	}

	// No specific command for this fix
	afs.logMessage("FIX_SKIP", "特定のコマンドがないためスキップ: "+fixAction)
	return true // Consider it attempted
}

// RunCommandWithAutoFix runs a command with auto-fix loop
func (afs *AutoFixSystem) RunCommandWithAutoFix(command []string, description string) bool {
	afs.retryCount = 0
	afs.fixHistory = []string{}

	afs.logMessage("START", fmt.Sprintf("%s: %s", description, strings.Join(command, " ")))

	for afs.retryCount < afs.maxRetries {
		// Create log file for this attempt
		logPath, logFile, err := afs.createLogFile(fmt.Sprintf("attempt_%d", afs.retryCount+1))
		if err != nil {
			afs.logMessage("ERROR", "ログファイル作成失敗: "+err.Error())
			return false
		}

		afs.logMessage("ATTEMPT", fmt.Sprintf("試行 %d/%d, ログ: %s", afs.retryCount+1, afs.maxRetries, logPath))

		// Execute command
		cmd := exec.Command(command[0], command[1:]...)

		// Capture both stdout and stderr
		stdoutPipe, _ := cmd.StdoutPipe()
		stderrPipe, _ := cmd.StderrPipe()

		// Start command
		if err := cmd.Start(); err != nil {
			afs.logMessage("ERROR", "コマンド起動失敗: "+err.Error())
			logFile.Close()

			fixAction := afs.analyzeError(err.Error())
			if !afs.executeFix(fixAction) {
				break
			}
			continue
		}

		// Read output in real-time
		go func() {
			scanner := bufio.NewScanner(stdoutPipe)
			for scanner.Scan() {
				line := scanner.Text()
				logFile.WriteString(line + "\n")
				afs.logMessage("OUTPUT", line)
			}
		}()

		go func() {
			scanner := bufio.NewScanner(stderrPipe)
			for scanner.Scan() {
				line := scanner.Text()
				logFile.WriteString("ERROR: " + line + "\n")
				afs.logMessage("ERROR", line)

				// Analyze error in real-time
				fixAction := afs.analyzeError(line)
				afs.executeFix(fixAction)
			}
		}()

		// Wait for command to complete
		err = cmd.Wait()
		logFile.Close()

		if err == nil {
			afs.logMessage("SUCCESS", "コマンド成功")
			afs.saveFixHistory()
			return true
		}

		// Command failed
		afs.logMessage("FAILED", fmt.Sprintf("コマンド失敗: %v", err))

		// Check if we should retry
		if afs.retryCount >= afs.maxRetries-1 {
			afs.logMessage("MAX_RETRIES", "最大リトライ回数に達しました")
			break
		}

		// Wait before retry (exponential backoff)
		waitTime := time.Duration(1<<uint(afs.retryCount)) * time.Second
		afs.logMessage("WAIT", fmt.Sprintf("%v 待機して再試行", waitTime))
		time.Sleep(waitTime)
	}

	afs.saveFixHistory()
	afs.logMessage("FINAL_FAILURE", "すべての修正試行が失敗しました")
	return false
}

// saveFixHistory saves fix history to file
func (afs *AutoFixSystem) saveFixHistory() {
	historyPath := filepath.Join(afs.logDir, "fix_history.log")

	content := fmt.Sprintf("=== 修正履歴 %s ===\n", time.Now().Format("2006-01-02 15:04:05"))
	content += fmt.Sprintf("最終エラー: %s\n", afs.lastError)
	content += fmt.Sprintf("試行回数: %d/%d\n", afs.retryCount, afs.maxRetries)
	content += "修正試行:\n"

	for _, attempt := range afs.fixHistory {
		content += "  - " + attempt + "\n"
	}
	content += "====================\n\n"

	if f, err := os.OpenFile(historyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		f.WriteString(content)
		f.Close()
	}
}

// BuildWithAutoFix builds the project with auto-fix
func BuildWithAutoFix() bool {
	afs := NewAutoFixSystem()

	// Try build.sh first (Linux), then build.bat (Windows), then go build directly
	buildCommands := [][]string{
		{"/bin/bash", "build.sh"},
		{"cmd", "/c", "build.bat"},
		{"go", "build", "-o", "streamdeck-twitch", "."},
	}

	for _, cmd := range buildCommands {
		if _, err := exec.LookPath(cmd[0]); err == nil {
			return afs.RunCommandWithAutoFix(cmd, "ビルド")
		}
	}

	// Fallback to go build
	return afs.RunCommandWithAutoFix([]string{"go", "build", "-o", "streamdeck-twitch", "."}, "ビルド")
}

// RunWithAutoFix runs the application with auto-fix
func RunWithAutoFix() bool {
	afs := NewAutoFixSystem()

	// Check if binary exists
	if _, err := os.Stat("streamdeck-twitch"); os.IsNotExist(err) {
		afs.logMessage("WARN", "実行ファイルが見つかりません。ビルドを試みます。")
		if !BuildWithAutoFix() {
			return false
		}
	}

	// Make executable
	os.Chmod("streamdeck-twitch", 0755)

	// Run with auto-fix
	return afs.RunCommandWithAutoFix([]string{"./streamdeck-twitch"}, "実行")
}
