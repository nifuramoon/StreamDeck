package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// Config represents the application configuration
type Config struct {
	ClientID             string `json:"client_id"`
	ClientSecret         string `json:"client_secret"`
	Scope                string `json:"scope"`
	NotificationsEnabled bool   `json:"notifications_enabled,omitempty"`
}

var (
	configDir  string
	configPath string
)

// initConfig initializes configuration paths
func initConfig() {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		userConfigDir = os.TempDir()
	}

	configDir = filepath.Join(userConfigDir, "streamdeck-twitch")
	configPath = filepath.Join(configDir, "config.json")
	os.MkdirAll(configDir, 0755)
}

// loadConfig loads configuration from file
func loadConfig() bool {
	initConfig()

	// Load main config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return false
	}

	// Update global variables if environment variables are not set
	if CID == "" && config.ClientID != "" {
		CID = config.ClientID
	}
	if CS == "" && config.ClientSecret != "" {
		CS = config.ClientSecret
	}
	if SCOPE == "" && config.Scope != "" {
		SCOPE = config.Scope
	}

	// Load notification setting
	if config.NotificationsEnabled {
		notificationEnabled = true
	}

	// Try to load tokens
	tokensPath := filepath.Join(configDir, "tokens.json")
	if tokenData, err := os.ReadFile(tokensPath); err == nil {
		var tokens map[string]string
		if err := json.Unmarshal(tokenData, &tokens); err == nil {
			if AT == "" && tokens["access_token"] != "" {
				AT = tokens["access_token"]
			}
			if RT == "" && tokens["refresh_token"] != "" {
				RT = tokens["refresh_token"]
			}
			if UID == "" && tokens["user_id"] != "" {
				UID = tokens["user_id"]
			}
			log.Printf("[Config] トークンを設定ファイルから読み込みました: %s", tokensPath)
		}
	}

	log.Printf("[Config] 設定ファイルを読み込みました: %s", configPath)
	return true
}

// saveConfig saves configuration to file
func saveConfig(config Config) bool {
	initConfig()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		log.Printf("[Config] 設定のマーシャリングエラー: %v", err)
		return false
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		log.Printf("[Config] 設定ファイルの書き込みエラー: %v", err)
		return false
	}

	log.Printf("[Config] 設定を保存しました: %s", configPath)
	return true
}

// saveNotificationSetting saves the notification setting to config file
func saveNotificationSetting(enabled bool) bool {
	initConfig()

	// Load existing config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return false
	}

	// Update notification setting
	config.NotificationsEnabled = enabled

	// Save back
	return saveConfig(config)
}

// createDefaultConfig creates a default config file with instructions
func createDefaultConfig() bool {
	initConfig()

	config := Config{
		ClientID:             "",
		ClientSecret:         "",
		Scope:                "user:read:email user:read:follows user:read:broadcast user:write:chat chat:read",
		NotificationsEnabled: true, // デフォルトで通知を有効にする
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return false
	}

	// Add instructions as comments (JSON doesn't support comments, so we'll create a separate file)
	instructions := `# Stream Deck Twitch 設定ファイル
# このファイルを編集してTwitch APIの認証情報を設定してください
# 
# 1. Twitch Developer Consoleでアプリケーションを作成:
#    https://dev.twitch.tv/console/apps/create
# 
# 2. Client IDとClient Secretを取得
# 
# 3. 以下の値を設定:
#    - client_id: あなたのClient ID
#    - client_secret: あなたのClient Secret
#    - scope: 必要なスコープ（デフォルト: "user:read:email"）
# 
# 4. アプリケーションを再起動
# 
# 設定例:
# {
#   "client_id": "your_client_id_here",
#   "client_secret": "your_client_secret_here",
#   "scope": "user:read:email"
# }
`

	// Save instructions file
	instructionsPath := filepath.Join(configDir, "INSTRUCTIONS.txt")
	os.WriteFile(instructionsPath, []byte(instructions), 0644)

	// Save config file
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return false
	}

	log.Printf("[Config] デフォルト設定ファイルを作成しました: %s", configPath)
	log.Printf("[Config] 設定手順: %s", instructionsPath)
	return true
}

// checkAndSetupConfig checks if configuration exists and helps user set it up
func checkAndSetupConfig() bool {
	// First, try to load from config file
	if loadConfig() {
		return true
	}

	// Check if we have default values (not empty strings)
	hasDefaults := CID != "" && CID != "zl3bbnc9ja0mdawfba3rar9jokjb0f" &&
		CS != "" && CS != "vo9ks19oyb8x2uha040245pj9s2klv"

	// If we have defaults from environment or the function itself, we're good
	if CID != "" && CS != "" && hasDefaults {
		// Helper function to get min of two ints
		minLen := 8
		if len(CID) < 8 {
			minLen = len(CID)
		}
		log.Printf("[Config] Using provided Client ID: %s...", CID[:minLen])
		return true
	}

	// Config file doesn't exist and no defaults, create default with instructions
	log.Println("")
	log.Println("================================================")
	log.Println("🎮 Stream Deck Twitch 初回セットアップ")
	log.Println("================================================")
	log.Println("")
	log.Println("設定ファイルが見つかりません。デフォルト設定を作成します。")
	log.Println("")

	if !createDefaultConfig() {
		log.Println("❌ 設定ファイルの作成に失敗しました")
		return false
	}

	log.Println("")
	log.Println("✅ 設定ファイルを作成しました！")
	log.Println("")
	log.Println("以下の手順で設定を完了してください:")
	log.Println("1. 設定ファイルを編集:")
	log.Printf("   %s", configPath)
	log.Println("2. Client IDとClient Secretを入力")
	log.Println("3. アプリケーションを再起動")
	log.Println("")
	log.Println("または、環境変数を設定することもできます:")
	log.Println("   export TWITCH_CLIENT_ID=your_client_id")
	log.Println("   export TWITCH_CLIENT_SECRET=your_client_secret")
	log.Println("================================================")

	return false
}
