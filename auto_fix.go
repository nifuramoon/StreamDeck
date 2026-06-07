package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// FixRule maps error pattern to fix command
type FixRule struct {
	pattern string
	cmd     []string
	desc    string
}

var fixRules = []FixRule{
	{"undefined:", {"go", "mod", "tidy"}, "undefined symbol"},
	{"cannot find package", {"go", "get", "./..."}, "missing package"},
	{"imported and not used", {"go", "fmt", "./..."}, "unused import"},
	{"declared and not used", {"go", "vet", "./..."}, "unused variable"},
	{"missing go.sum entry", {"go", "mod", "tidy"}, "stale go.sum"},
	{"no such file or directory", {"ls", "-la"}, "missing file"},
	{"permission denied", {"chmod", "+x", "build.sh"}, "permission denied"},
	{"Stream Deck:", {"sudo", "usbreset", "0fd9:006d"}, "USB reconnect"},
	{"401", {"rm", "-f", filepath.Join(configDir, "tokens.json")}, "token refresh"},
	{"404", nil, "check URL"},
	{"429", nil, "rate limited - wait"},
}

type AutoFixSystem struct {
	logDir string
}

func NewAutoFixSystem() *AutoFixSystem {
	d := filepath.Join(".", "logs")
	os.MkdirAll(d, 0755)
	return &AutoFixSystem{logDir: d}
}

func (afs *AutoFixSystem) logf(category, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	t := time.Now().Format("2006-01-02 15:04:05")
	entry := fmt.Sprintf("[%s] [%s] %s\n", t, category, msg)

	// Append to latest.log
	p := filepath.Join(afs.logDir, "latest.log")
	if f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		f.WriteString(entry)
		f.Close()
	}
	log.Printf("[AutoFix] %s: %s", category, msg)
}

func (afs *AutoFixSystem) findFix(errMsg string) []string {
	for _, r := range fixRules {
		if strings.Contains(errMsg, r.pattern) {
			afs.logf("ANALYSIS", "%s -> %s", r.pattern, r.desc)
			return r.cmd
		}
	}
	afs.logf("ANALYSIS", "no specific fix for: %s", errMsg)
	return nil
}

func (afs *AutoFixSystem) tryFix(cmd []string) bool {
	if cmd == nil {
		afs.logf("SKIP", "no command for this error")
		return false
	}
	afs.logf("EXEC", "%s", strings.Join(cmd, " "))
	out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		afs.logf("FAIL", "%s: %v", out, err)
		return false
	}
	afs.logf("OK", "%s", out)
	return true
}

func (afs *AutoFixSystem) RunWithAutoFix(cmd []string, desc string) bool {
	afs.logf("START", "%s: %s", desc, strings.Join(cmd, " "))

	for attempt := 1; attempt <= 5; attempt++ {
		afs.logf("ATTEMPT", "%d/5", attempt)

		out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
		if err == nil {
			afs.logf("SUCCESS", "done")
			return true
		}

		errStr := string(out)
		afs.logf("ERROR", "%s", errStr)

		if attempt == 5 {
			break
		}

		fixCmd := afs.findFix(errStr)
		if !afs.tryFix(fixCmd) {
			afs.logf("GIVEUP", "fix failed, stopping")
			break
		}

		// Exponential backoff
		wait := time.Duration(1<<uint(attempt-1)) * time.Second
		afs.logf("WAIT", "%v before retry", wait)
		time.Sleep(wait)
	}

	afs.logf("FAIL", "all attempts exhausted")
	return false
}

func BuildWithAutoFix() bool {
	afs := NewAutoFixSystem()

	// Try build script first
	for _, c := range [][]string{
		{"/bin/bash", "build.sh"},
		{"cmd", "/c", "build.bat"},
	} {
		if _, err := exec.LookPath(c[0]); err == nil {
			return afs.RunWithAutoFix(c, "build")
		}
	}
	// Fallback to go build
	return afs.RunWithAutoFix([]string{"go", "build", "-o", "streamdeck-twitch", "."}, "build")
}

func RunWithAutoFix() bool {
	afs := NewAutoFixSystem()

	if _, err := os.Stat("streamdeck-twitch"); os.IsNotExist(err) {
		afs.logf("WARN", "binary not found, building first")
		if !BuildWithAutoFix() {
			return false
		}
	}
	os.Chmod("streamdeck-twitch", 0755)
	return afs.RunWithAutoFix([]string{"./streamdeck-twitch"}, "run")
}