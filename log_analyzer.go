package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LogAnalyzer struct {
	logDir  string
	errors  map[string]int
	lastLog time.Time
}

func NewLogAnalyzer() *LogAnalyzer {
	h, _ := os.UserHomeDir()
	d := filepath.Join(h, ".cache", "streamdeck-twitch", "logs")
	os.MkdirAll(d, 0755)
	return &LogAnalyzer{logDir: d, errors: make(map[string]int)}
}

func (la *LogAnalyzer) LogError(typ, msg string) {
	logf("ANALYZER", "%s: %s", typ, msg)
	la.errors[typ]++
	la.writeLog("errors.log", fmt.Sprintf("[%s] %s: %s", time.Now().Format("2006-01-02 15:04:05"), typ, msg))
	la.suggest()
}

func (la *LogAnalyzer) LogAPIError(url string, status int, msg string) {
	la.LogError(fmt.Sprintf("API_%d", status), fmt.Sprintf("%s: %s", url, msg))
}

func (la *LogAnalyzer) LogTokenError(typ, msg string) {
	la.LogError("TOKEN_"+typ, msg)
}

func (la *LogAnalyzer) LogButtonPress(page string, idx int, label string) {
	la.writeLog("actions.log", fmt.Sprintf("[%s] %s:%d=%s", time.Now().Format("2006-01-02 15:04:05"), page, idx, label))
}

func (la *LogAnalyzer) writeLog(filename, entry string) {
	f, err := os.OpenFile(filepath.Join(la.logDir, filename), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(entry + "\n")
}

func (la *LogAnalyzer) suggest() {
	if time.Since(la.lastLog) < 5*time.Minute {
		return
	}
	la.lastLog = time.Now()

	for typ, count := range la.errors {
		if count < 3 {
			continue
		}
		switch {
		case strings.Contains(typ, "TOKEN_INVALID"), strings.Contains(typ, "TOKEN_EXPIRED"):
			warnLog("Token invalid/expired. Re-authenticate with OAuth.")
		case strings.Contains(typ, "API_401"):
			warnLog("API auth failed. Check Client ID and Access Token.")
		case strings.Contains(typ, "API_429"):
			warnLog("Rate limited. Reducing request frequency.")
		}
	}
}

func (la *LogAnalyzer) GetErrorSummary() string {
	if len(la.errors) == 0 {
		return "No errors"
	}
	s := "Recent errors:\n"
	for typ, n := range la.errors {
		s += fmt.Sprintf("  %s: %d\n", typ, n)
	}
	return s
}

func (la *LogAnalyzer) ClearLogs() {
	files, _ := filepath.Glob(filepath.Join(la.logDir, "*.log"))
	for _, f := range files {
		os.Remove(f)
	}
	la.errors = make(map[string]int)
}