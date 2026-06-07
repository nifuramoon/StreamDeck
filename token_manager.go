package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

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

type TokenManager struct {
	tokensFile string
	backupDir  string
	token      *TokenInfo
}

var configDir = func() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "streamdeck-twitch")
}()

func NewTokenManager() *TokenManager {
	d := filepath.Join(configDir, "backups")
	os.MkdirAll(d, 0755)
	return &TokenManager{
		tokensFile: filepath.Join(configDir, "tokens.json"),
		backupDir:  d,
	}
}

func (tm *TokenManager) SaveToken(t *TokenInfo) error {
	tm.token = t
	data, _ := json.MarshalIndent(t, "", "  ")
	if err := os.WriteFile(tm.tokensFile, data, 0600); err != nil {
		return err
	}
	// Backup
	backup := filepath.Join(tm.backupDir, fmt.Sprintf("tokens_%s_%s.json",
		t.LoginName, time.Now().Format("20060102_150405")))
	os.WriteFile(backup, data, 0600) // ignore backup error
	log.Printf("[Token] Saved for %s (%s), backup: %s", t.DisplayName, t.LoginName, backup)
	return nil
}

func (tm *TokenManager) LoadToken() (*TokenInfo, error) {
	// Try main file
	if t, err := tm.loadFile(tm.tokensFile); err == nil {
		tm.token = t
		log.Printf("[Token] Loaded for %s", t.LoginName)
		return t, nil
	}
	// Try latest backup
	entries, err := os.ReadDir(tm.backupDir)
	if err != nil {
		return nil, fmt.Errorf("no tokens found")
	}
	var latest os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			if latest == nil || e.Name() > latest.Name() {
				latest = e
			}
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("no valid backups")
	}
	t, err := tm.loadFile(filepath.Join(tm.backupDir, latest.Name()))
	if err != nil {
		return nil, err
	}
	tm.token = t
	log.Printf("[Token] Restored from backup: %s (%s)", latest.Name(), t.LoginName)
	return t, nil
}

func (tm *TokenManager) loadFile(path string) (*TokenInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t TokenInfo
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (tm *TokenManager) ValidateToken() (bool, string) {
	if tm.token == nil {
		return false, "No token loaded"
	}
	if time.Now().After(tm.token.ExpiresAt) {
		return false, "Token expired"
	}
	if time.Since(tm.token.CreatedAt) > 60*24*time.Hour {
		return false, "Token too old (>60 days)"
	}
	return true, "Valid"
}

func (tm *TokenManager) GetCurrentToken() *TokenInfo { return tm.token }

func (tm *TokenManager) UpdateLastUsed() {
	if tm.token == nil {
		return
	}
	tm.token.LastUsed = time.Now()
	data, _ := json.MarshalIndent(tm.token, "", "  ")
	os.WriteFile(tm.tokensFile, data, 0600)
}

func (tm *TokenManager) ListBackups() []string {
	entries, _ := os.ReadDir(tm.backupDir)
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, filepath.Join(tm.backupDir, e.Name()))
		}
	}
	sort.Strings(names)
	return names
}

func (tm *TokenManager) RestoreFromBackup(path string) error {
	t, err := tm.loadFile(path)
	if err != nil {
		return err
	}
	tm.token = t
	data, _ := json.MarshalIndent(t, "", "  ")
	if err := os.WriteFile(tm.tokensFile, data, 0600); err != nil {
		return err
	}
	log.Printf("[Token] Restored from: %s", filepath.Base(path))
	return nil
}