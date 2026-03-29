package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LogAnalyzer analyzes logs and suggests fixes
type LogAnalyzer struct {
	logDir      string
	errorCounts map[string]int
	lastAnalyze time.Time
}

// NewLogAnalyzer creates a new log analyzer
func NewLogAnalyzer() *LogAnalyzer {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	logDir := filepath.Join(home, ".cache", "streamdeck-twitch", "logs")
	os.MkdirAll(logDir, 0755)

	return &LogAnalyzer{
		logDir:      logDir,
		errorCounts: make(map[string]int),
		lastAnalyze: time.Now(),
	}
}

// LogError logs an error for analysis
func (la *LogAnalyzer) LogError(errorType, message string) {
	log.Printf("[LOG ANALYZER] Error detected: %s - %s", errorType, message)

	// Count error occurrences
	la.errorCounts[errorType]++

	// Write to log file
	logFile := filepath.Join(la.logDir, "errors.log")
	entry := fmt.Sprintf("[%s] %s: %s\n", time.Now().Format("2006-01-02 15:04:05"), errorType, message)

	if f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		defer f.Close()
		f.WriteString(entry)
	}

	// Analyze and suggest fixes if needed
	la.analyzeAndSuggest()
}

// LogAPIError logs API-related errors
func (la *LogAnalyzer) LogAPIError(url string, statusCode int, message string) {
	errorType := fmt.Sprintf("API_ERROR_%d", statusCode)
	errorMsg := fmt.Sprintf("URL: %s, Status: %d, Message: %s", url, statusCode, message)
	la.LogError(errorType, errorMsg)
}

// LogTokenError logs token-related errors
func (la *LogAnalyzer) LogTokenError(errorType, message string) {
	la.LogError("TOKEN_"+errorType, message)
}

// LogButtonPress logs button presses for user behavior analysis
func (la *LogAnalyzer) LogButtonPress(page string, buttonIndex int, buttonLabel string) {
	entry := fmt.Sprintf("[%s] BUTTON_PRESS: page=%s, button=%d, label=%s\n",
		time.Now().Format("2006-01-02 15:04:05"), page, buttonIndex, buttonLabel)

	logFile := filepath.Join(la.logDir, "user_actions.log")
	if f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		defer f.Close()
		f.WriteString(entry)
	}
}

// analyzeAndSuggest analyzes logs and suggests fixes
func (la *LogAnalyzer) analyzeAndSuggest() {
	// Only analyze every 5 minutes to avoid spam
	if time.Since(la.lastAnalyze) < 5*time.Minute {
		return
	}

	la.lastAnalyze = time.Now()

	// Read error log
	logFile := filepath.Join(la.logDir, "errors.log")
	data, err := os.ReadFile(logFile)
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	recentErrors := make(map[string]int)

	// Count recent errors (last 24 hours)
	for _, line := range lines {
		if strings.Contains(line, "TOKEN_") || strings.Contains(line, "API_ERROR_") {
			parts := strings.SplitN(line, ":", 3)
			if len(parts) >= 3 {
				errorType := strings.TrimSpace(parts[1])
				recentErrors[errorType]++
			}
		}
	}

	// Suggest fixes based on error patterns
	la.suggestFixes(recentErrors)
}

// suggestFixes suggests fixes based on error patterns
func (la *LogAnalyzer) suggestFixes(errorCounts map[string]int) {
	suggestions := []string{}

	// Check for common error patterns
	for errorType, count := range errorCounts {
		if count >= 3 { // If error occurs 3+ times, suggest fix
			switch {
			case strings.Contains(errorType, "TOKEN_INVALID"):
				suggestions = append(suggestions, "Token appears to be invalid. Try re-authenticating with OAuth.")
			case strings.Contains(errorType, "TOKEN_EXPIRED"):
				suggestions = append(suggestions, "Token has expired. Use token refresh or re-authenticate.")
			case strings.Contains(errorType, "API_ERROR_401"):
				suggestions = append(suggestions, "API authentication failed. Check Client ID and Access Token.")
			case strings.Contains(errorType, "API_ERROR_403"):
				suggestions = append(suggestions, "API permission denied. Check OAuth scopes and user permissions.")
			case strings.Contains(errorType, "API_ERROR_429"):
				suggestions = append(suggestions, "API rate limit exceeded. Reducing request frequency.")
			case strings.Contains(errorType, "MISSING_SCOPE"):
				suggestions = append(suggestions, "Missing required OAuth scopes. Update scope in config file.")
			}
		}
	}

	// Apply automatic fixes if possible
	la.applyAutomaticFixes(errorCounts)

	// Log suggestions
	if len(suggestions) > 0 {
		log.Println("[LOG ANALYZER] Suggested fixes:")
		for i, suggestion := range suggestions {
			log.Printf("[LOG ANALYZER] %d. %s", i+1, suggestion)
		}
	}
}

// applyAutomaticFixes applies fixes that can be done automatically
func (la *LogAnalyzer) applyAutomaticFixes(errorCounts map[string]int) {
	for errorType, count := range errorCounts {
		if count >= 5 { // Only apply automatic fixes for persistent errors
			switch {
			case strings.Contains(errorType, "MISSING_SCOPE"):
				la.fixMissingScopes()
			case strings.Contains(errorType, "TOKEN_EXPIRED") && RT != "" && CS != "":
				la.attemptTokenRefresh()
			}
		}
	}
}

// fixMissingScopes attempts to fix missing OAuth scopes
func (la *LogAnalyzer) fixMissingScopes() {
	log.Println("[LOG ANALYZER] Attempting to fix missing scopes...")

	// Check current config
	config := loadConfigFromFile()
	if config.Scope == "" {
		config.Scope = "user:read:email user:read:follows user:read:broadcast"
		saveConfig(config)
		log.Println("[LOG ANALYZER] Updated scope in config file")
	}

	// If token exists but has wrong scope, suggest re-auth
	if AT != "" && tokenManager != nil {
		token := tokenManager.GetCurrentToken()
		if token != nil && !strings.Contains(token.Scope, "user:read:follows") {
			log.Println("[LOG ANALYZER] Current token missing required scopes. Please re-authenticate.")
			showTokenError("Missing required scopes. Please re-authenticate with OAuth.")
		}
	}
}

// attemptTokenRefresh attempts to refresh an expired token
func (la *LogAnalyzer) attemptTokenRefresh() {
	log.Println("[LOG ANALYZER] Attempting automatic token refresh...")

	if RT == "" || CS == "" || CID == "" {
		log.Println("[LOG ANALYZER] Cannot refresh: missing refresh token or client secret")
		return
	}

	// Try to refresh token
	req, _ := http.NewRequest("POST", "https://id.twitch.tv/oauth2/token", strings.NewReader(url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {RT},
		"client_id":     {CID},
		"client_secret": {CS},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[LOG ANALYZER] Refresh failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)

		if newToken, ok := result["access_token"].(string); ok {
			AT = newToken
			if newRefresh, ok := result["refresh_token"].(string); ok {
				RT = newRefresh
			}

			// Update token manager
			if tokenManager != nil && tokenManager.GetCurrentToken() != nil {
				token := tokenManager.GetCurrentToken()
				token.AccessToken = AT
				token.RefreshToken = RT
				token.ExpiresAt = time.Now().Add(24 * time.Hour)
				tokenManager.SaveToken(token)
			}

			log.Println("[LOG ANALYZER] Token refresh successful!")
		}
	} else {
		log.Printf("[LOG ANALYZER] Refresh failed with status: %d", resp.StatusCode)
	}
}

// loadConfigFromFile loads config from file
func loadConfigFromFile() Config {
	configPath := filepath.Join(getConfigDir(), "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}
	}

	var config Config
	json.Unmarshal(data, &config)
	return config
}

// GetErrorSummary returns a summary of recent errors
func (la *LogAnalyzer) GetErrorSummary() string {
	if len(la.errorCounts) == 0 {
		return "No errors detected"
	}

	summary := "Recent errors:\n"
	for errorType, count := range la.errorCounts {
		summary += fmt.Sprintf("  %s: %d occurrences\n", errorType, count)
	}
	return summary
}

// ClearLogs clears all log files
func (la *LogAnalyzer) ClearLogs() error {
	files, _ := filepath.Glob(filepath.Join(la.logDir, "*.log"))
	for _, file := range files {
		os.Remove(file)
	}
	la.errorCounts = make(map[string]int)
	return nil
}
