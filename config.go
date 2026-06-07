package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

type Config struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Scope        string `json:"scope"`
	Notify       bool   `json:"notifications_enabled,omitempty"`
}

var configPath string

func initConfig() {
	c, _ := os.UserConfigDir()
	configPath = filepath.Join(c, "streamdeck-twitch", "config.json")
	os.MkdirAll(filepath.Dir(configPath), 0755)
}

func loadConfig() bool {
	initConfig()
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false
	}
	if cfg.ClientID != "" {
		CID = cfg.ClientID
	}
	if cfg.ClientSecret != "" {
		CS = cfg.ClientSecret
	}
	if cfg.Scope != "" {
		SCOPE = cfg.Scope
	} else if SCOPE == "" {
		SCOPE = "user:read:email user:read:follows user:read:broadcast user:write:chat chat:read"
	}
	return true
}

func saveConfig(cfg Config) bool {
	initConfig()
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(configPath, data, 0600) == nil
}

func maskString(s string) string {
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "..." + s[len(s)-4:]
}

func checkAndSetupConfig() bool {
	if loadConfig() {
		return true
	}
	if os.Getenv("TWITCH_CLIENT_ID") != "" || os.Getenv("TWITCH_CLIENT_SECRET") != "" {
		infoLog("Using environment variables")
		return true
	}
	if CID != "" && CS != "" {
		return true
	}

	infoLog("First time setup")
	if err := os.WriteFile(configPath, []byte(`{
  "client_id": "",
  "client_secret": "",
  "scope": "user:read:email user:read:follows user:read:broadcast user:write:chat chat:read"
}`), 0600); err != nil {
		return false
	}
	logf("INFO", "Created config: %s", configPath)
	logf("INFO", "Edit it with your Twitch credentials, or set TWITCH_CLIENT_ID/SECRET env vars")
	return false
}