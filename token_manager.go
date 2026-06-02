package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// TokenInfo holds token information with metadata
type TokenInfo struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	UserID       string    `json:"user_id"`
	LoginName    string    `json:"login_name"`
	DisplayName  string    `json:"display_name"`
	ClientID     string    `json:"client_id"`
	Scope        string    `json:"scope"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
	LastUsed     time.Time `json:"last_used"`
}

// TokenManager handles token backup, restore, and validation
type TokenManager struct {
	configDir    string
	tokensFile   string
	backupDir    string
	currentToken *TokenInfo
}

// NewTokenManager creates a new token manager
func NewTokenManager() *TokenManager {
	configDir := getConfigDir()
	backupDir := filepath.Join(configDir, "backups")

	// Create backup directory if it doesn't exist
	os.MkdirAll(backupDir, 0755)

	return &TokenManager{
		configDir:  configDir,
		tokensFile: filepath.Join(configDir, "tokens.json"),
		backupDir:  backupDir,
	}
}

// SaveToken saves a new token and creates a backup
func (tm *TokenManager) SaveToken(token *TokenInfo) error {
	tm.currentToken = token

	// Save to main tokens file
	if err := tm.saveToFile(tm.tokensFile, token); err != nil {
		return fmt.Errorf("failed to save token: %v", err)
	}

	// Create timestamped backup
	backupFile := filepath.Join(tm.backupDir,
		fmt.Sprintf("tokens_%s_%s.json",
			token.LoginName,
			time.Now().Format("20060102_150405")))

	if err := tm.saveToFile(backupFile, token); err != nil {
		log.Printf("[Token] Warning: failed to create backup: %v", err)
	}

	log.Printf("[Token] Token saved for user: %s (%s)", token.DisplayName, token.LoginName)
	log.Printf("[Token] Backup created: %s", backupFile)

	return nil
}

// LoadToken loads the most recent token
func (tm *TokenManager) LoadToken() (*TokenInfo, error) {
	// Try to load from main file first
	if data, err := os.ReadFile(tm.tokensFile); err == nil {
		var token TokenInfo
		if err := json.Unmarshal(data, &token); err == nil {
			tm.currentToken = &token
			log.Printf("[Token] Loaded token for user: %s", token.LoginName)
			return &token, nil
		}
	}

	// If main file doesn't exist or is corrupted, try to find latest backup
	backups, err := filepath.Glob(filepath.Join(tm.backupDir, "tokens_*.json"))
	if err != nil || len(backups) == 0 {
		return nil, fmt.Errorf("no tokens found")
	}

	// Find the latest backup
	var latestBackup string
	var latestTime time.Time

	for _, backup := range backups {
		info, err := os.Stat(backup)
		if err != nil {
			continue
		}

		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latestBackup = backup
		}
	}

	if latestBackup == "" {
		return nil, fmt.Errorf("no valid backups found")
	}

	// Load from backup
	data, err := os.ReadFile(latestBackup)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup: %v", err)
	}

	var token TokenInfo
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to parse backup: %v", err)
	}

	tm.currentToken = &token
	log.Printf("[Token] Restored token from backup: %s (user: %s)",
		filepath.Base(latestBackup), token.LoginName)

	return &token, nil
}

// ValidateToken checks if the current token is valid
func (tm *TokenManager) ValidateToken() (bool, string) {
	if tm.currentToken == nil {
		return false, "No token loaded"
	}

	// Check if token is expired
	if time.Now().After(tm.currentToken.ExpiresAt) {
		return false, fmt.Sprintf("Token expired at %s",
			tm.currentToken.ExpiresAt.Format("2006-01-02 15:04:05"))
	}

	// Check if token was created more than 60 days ago (typical max lifetime)
	if time.Since(tm.currentToken.CreatedAt) > 60*24*time.Hour {
		return false, fmt.Sprintf("Token too old (created at %s)",
			tm.currentToken.CreatedAt.Format("2006-01-02"))
	}

	return true, "Token is valid"
}

// GetCurrentToken returns the current token
func (tm *TokenManager) GetCurrentToken() *TokenInfo {
	return tm.currentToken
}

// UpdateLastUsed updates the last used timestamp
func (tm *TokenManager) UpdateLastUsed() {
	if tm.currentToken != nil {
		tm.currentToken.LastUsed = time.Now()
		// Save the update
		tm.saveToFile(tm.tokensFile, tm.currentToken)
	}
}

// ListBackups returns a list of all backups
func (tm *TokenManager) ListBackups() []string {
	backups, _ := filepath.Glob(filepath.Join(tm.backupDir, "tokens_*.json"))
	return backups
}

// RestoreFromBackup restores a token from a specific backup file
func (tm *TokenManager) RestoreFromBackup(backupFile string) error {
	data, err := os.ReadFile(backupFile)
	if err != nil {
		return fmt.Errorf("failed to read backup: %v", err)
	}

	var token TokenInfo
	if err := json.Unmarshal(data, &token); err != nil {
		return fmt.Errorf("failed to parse backup: %v", err)
	}

	tm.currentToken = &token

	// Save to main file
	if err := tm.saveToFile(tm.tokensFile, &token); err != nil {
		return fmt.Errorf("failed to save restored token: %v", err)
	}

	log.Printf("[Token] Restored token from: %s", filepath.Base(backupFile))
	return nil
}

// saveToFile saves token to a file
func (tm *TokenManager) saveToFile(filename string, token *TokenInfo) error {
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0600)
}

// getConfigDir returns the configuration directory
func getConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "streamdeck-twitch")
}
