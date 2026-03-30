package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuth関連のグローバル変数
var (
	// Note: oauthClientID and oauthClientSecret are now retrieved from CID and CS at runtime
	oauthScope        = "user:read:email user:read:follows user:read:broadcast" // 必要なスコープ
	oauthCode         = ""
	oauthAccessToken  = AT
	oauthRefreshToken = RT
	oauthUserID       = UID
	oauthLoginName    = ""
	oauthDisplayName  = ""

	// 環境変数がなくてもOAuthを試行できるようにするための一時変数
	tempClientID     = ""
	tempClientSecret = ""
)

// startOAuth starts the OAuth flow by opening browser
func startOAuth() {
	// Check if we have client ID and secret
	clientID := CID
	clientSecret := CS

	log.Printf("[OAuth Debug] CID='%s', CS='%s', SCOPE='%s'", CID, CS, SCOPE)
	log.Printf("[OAuth Debug] tempClientID='%s', tempClientSecret='%s'", tempClientID, tempClientSecret)

	// If global variables are empty, check temporary ones
	if clientID == "" {
		clientID = tempClientID
		log.Printf("[OAuth Debug] Using tempClientID: %s", clientID)
	}
	if clientSecret == "" {
		clientSecret = tempClientSecret
		log.Printf("[OAuth Debug] Using tempClientSecret: %s", maskString(clientSecret))
	}

	if clientID == "" {
		log.Println("================================================")
		log.Println("[OAuth] ERROR: Client ID not set")
		log.Println("[OAuth] CID variable value: ", CID)
		log.Println("[OAuth] tempClientID value: ", tempClientID)
		log.Println("[OAuth] Check config file at: ~/.config/streamdeck-twitch/config.json")
		log.Println("[OAuth] Restart application and run interactive setup")
		log.Println("================================================")
		return
	}

	if clientID == "" {
		log.Println("================================================")
		log.Println("[OAuth] ERROR: Client ID not set")
		log.Println("[OAuth] Restart application and run interactive setup")
		log.Println("================================================")
		return
	}

	// Use scope from config if available, otherwise use default oauthScope
	scope := oauthScope
	if SCOPE != "" {
		scope = SCOPE
		log.Printf("[OAuth Debug] Using scope: %s", scope)
	} else {
		log.Printf("[OAuth Debug] Using default oauthScope: %s", scope)
	}

	// Check if scope is complete, if not, add missing scopes
	scope = ensureCompleteScope(scope)

	authURL := fmt.Sprintf(
		"https://id.twitch.tv/oauth2/authorize?client_id=%s&redirect_uri=http://localhost:8080&response_type=code&scope=%s",
		clientID, url.QueryEscape(scope))

	log.Printf("[OAuth] Opening browser: %s", authURL)
	platformOpenBrowser(authURL)
	log.Println("[OAuth] After browser authentication, copy the authorization code and press 'Get' button")
}

// ensureCompleteScope ensures the scope contains all required permissions
func ensureCompleteScope(scope string) string {
	requiredScopes := []string{
		"user:read:email",
		"user:read:follows",
		"user:read:broadcast",
	}

	scopeParts := strings.Split(scope, " ")
	scopeMap := make(map[string]bool)

	for _, part := range scopeParts {
		scopeMap[part] = true
	}

	// Add missing scopes
	for _, required := range requiredScopes {
		if !scopeMap[required] {
			scopeParts = append(scopeParts, required)
			log.Printf("[OAuth] Added missing scope: %s", required)

			// Log to analyzer
			if logAnalyzer != nil {
				logAnalyzer.LogError("MISSING_SCOPE", fmt.Sprintf("Added missing scope: %s", required))
			}
		}
	}

	return strings.Join(scopeParts, " ")
}

// getTokenFromClipboard gets code from clipboard and exchanges for tokens
func getTokenFromClipboard() {
	// クリップボードからコードを取得
	code, err := getClipboardText()
	if err != nil {
		log.Printf("[OAuth] クリップボード取得エラー: %v", err)
		return
	}

	// URLからcodeパラメータを抽出
	if strings.Contains(code, "code=") {
		if u, err := url.Parse(code); err == nil {
			if c := u.Query().Get("code"); c != "" {
				code = c
			}
		}
	}

	code = strings.TrimSpace(code)
	if code == "" {
		log.Println("[OAuth] エラー: クリップボードに認可コードが見つかりません")
		return
	}

	log.Printf("[OAuth] 認可コード取得: %s...", code[:min(10, len(code))])
	exchangeCodeForTokens(code)
}

// exchangeCodeForTokens exchanges authorization code for access tokens
func exchangeCodeForTokens(code string) {
	// 使用するClient IDとSecretを決定
	clientID := CID
	clientSecret := CS

	// 環境変数がない場合は一時変数を使用（現時点では一時変数は空）
	if clientID == "" || clientSecret == "" {
		log.Println("[OAuth] エラー: Client IDまたはClient Secretが設定されていません")
		log.Println("[OAuth] 環境変数 TWITCH_CLIENT_ID と TWITCH_CLIENT_SECRET を設定してください")
		return
	}

	log.Println("[OAuth] アクセストークンを取得中...")

	resp, err := http.PostForm("https://id.twitch.tv/oauth2/token", url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {"http://localhost:8080"},
	})

	if err != nil {
		log.Printf("[OAuth] トークン取得エラー: %v", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[OAuth] ステータスコード: %d", resp.StatusCode)

	if resp.StatusCode == 200 {
		var result map[string]interface{}
		json.Unmarshal(body, &result)

		oauthAccessToken = fmt.Sprintf("%v", result["access_token"])
		oauthRefreshToken = fmt.Sprintf("%v", result["refresh_token"])

		log.Println("[OAuth] アクセストークン取得成功！")
		log.Printf("[OAuth] アクセストークン: %s...", oauthAccessToken[:min(10, len(oauthAccessToken))])
		log.Printf("[OAuth] リフレッシュトークン: %s...", oauthRefreshToken[:min(10, len(oauthRefreshToken))])

		// グローバル変数も更新
		AT = oauthAccessToken
		RT = oauthRefreshToken

		// ユーザー情報を取得
		getUserInfo()
	} else {
		log.Printf("[OAuth] エラー: %d", resp.StatusCode)
		log.Printf("[OAuth] エラーメッセージ: %s", string(body))
	}
}

// getUserInfo gets user info using the access token
func getUserInfo() {
	if CID == "" || oauthAccessToken == "" {
		return
	}

	log.Println("[OAuth] ユーザー情報取得中...")
	req, _ := http.NewRequest("GET", "https://api.twitch.tv/helix/users", nil)
	req.Header.Set("Client-ID", CID)
	req.Header.Set("Authorization", "Bearer "+oauthAccessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[OAuth] ユーザー情報取得エラー: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var userData map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&userData)

		if data, ok := userData["data"].([]interface{}); ok && len(data) > 0 {
			user, _ := data[0].(map[string]interface{})
			oauthUserID = fmt.Sprintf("%v", user["id"])
			oauthDisplayName = fmt.Sprintf("%v", user["display_name"])
			oauthLoginName = fmt.Sprintf("%v", user["login"])

			log.Printf("[OAuth] User ID: %s", oauthUserID)
			log.Printf("[OAuth] Display name: %s", oauthDisplayName)
			log.Printf("[OAuth] Login name: %s", oauthLoginName)

			// グローバル変数も更新
			UID = oauthUserID
		}
	} else {
		log.Printf("[OAuth] ユーザー情報取得失敗: %d", resp.StatusCode)
	}
}

// saveEnvVars saves OAuth tokens to environment variables, config file, and token manager
func saveEnvVars() {
	envCount := 0
	configCount := 0

	log.Printf("[OAuth Debug] saveEnvVars called: CID='%s', CS='%s', tempClientID='%s', tempClientSecret='%s'",
		maskString(CID), maskString(CS), tempClientID, maskString(tempClientSecret))

	// Determine which values to save
	// Priority: temp values > current global values
	saveClientID := CID
	saveClientSecret := CS

	if tempClientID != "" {
		saveClientID = tempClientID
		log.Printf("[OAuth] Using tempClientID for save: %s", maskString(tempClientID))
	}
	if tempClientSecret != "" {
		saveClientSecret = tempClientSecret
		log.Printf("[OAuth] Using tempClientSecret for save")
	}

	// Save to environment variables
	if saveClientID != "" && setEnvVar("TWITCH_CLIENT_ID", saveClientID) {
		log.Println("[OAuth] Saved Client ID to environment")
		envCount++
	}
	if saveClientSecret != "" && setEnvVar("TWITCH_CLIENT_SECRET", saveClientSecret) {
		log.Println("[OAuth] Saved Client Secret to environment")
		envCount++
	}
	if SCOPE != "" && setEnvVar("TWITCH_SCOPE", SCOPE) {
		envCount++
	}
	if oauthAccessToken != "" && setEnvVar("TWITCH_ACCESS_TOKEN", oauthAccessToken) {
		envCount++
	}
	if oauthRefreshToken != "" && setEnvVar("TWITCH_REFRESH_TOKEN", oauthRefreshToken) {
		envCount++
	}
	if oauthUserID != "" && setEnvVar("TWITCH_USER_ID", oauthUserID) {
		envCount++
	}

	// Save to config file, but preserve existing settings
	// Load existing config first
	existingConfig := loadConfigFromFile()
	if existingConfig.ClientID == "" && existingConfig.ClientSecret == "" {
		// No existing config, create new one
		config := Config{
			ClientID:     saveClientID,
			ClientSecret: saveClientSecret,
			Scope:        SCOPE,
		}

		// Check if we're trying to save default values
		isDefaultCID := saveClientID == "zl3bbnc9ja0mdawfba3rar9jokjb0f"
		isDefaultCS := saveClientSecret == "vo9ks19oyb8x2uha040245pj9s2klv"

		if isDefaultCID || isDefaultCS {
			log.Println("[OAuth] Warning: Trying to save default values to config. Skipping config save.")
			log.Println("[OAuth] Please enter your actual Twitch Client ID and Secret first.")
		} else if saveConfig(config) {
			configCount++
			log.Println("[OAuth] Created new config file with provided credentials")
		}
	} else {
		// Update existing config ONLY for Client ID/Secret (not scope)
		// and ONLY if they're not default values
		updated := false

		if saveClientID != "" && saveClientID != "zl3bbnc9ja0mdawfba3rar9jokjb0f" && existingConfig.ClientID != saveClientID {
			existingConfig.ClientID = saveClientID
			updated = true
			log.Println("[OAuth] Updated Client ID in config")
		}

		if saveClientSecret != "" && saveClientSecret != "vo9ks19oyb8x2uha040245pj9s2klv" && existingConfig.ClientSecret != saveClientSecret {
			existingConfig.ClientSecret = saveClientSecret
			updated = true
			log.Println("[OAuth] Updated Client Secret in config")
		}

		// NEVER update scope from token scope in config.json
		// Config scope is user's desired permissions, token scope is granted permissions
		log.Printf("[OAuth Debug] Config scope preserved (not updated from token): %s", existingConfig.Scope)

		if updated {
			if saveConfig(existingConfig) {
				configCount++
				log.Println("[OAuth] Updated existing config file")
			}
		} else {
			log.Println("[OAuth] No changes to save in config file")
		}
	}

	// Save to token manager with backup (if we have tokens)
	if oauthAccessToken != "" {
		if tokenManager == nil {
			tokenManager = NewTokenManager()
		}

		tokenInfo := &TokenInfo{
			AccessToken:  oauthAccessToken,
			RefreshToken: oauthRefreshToken,
			UserID:       oauthUserID,
			LoginName:    oauthLoginName,
			DisplayName:  oauthDisplayName,
			ClientID:     CID,
			Scope:        SCOPE,
			ExpiresAt:    time.Now().Add(24 * time.Hour), // Twitch tokens typically last 24 hours
			CreatedAt:    time.Now(),
			LastUsed:     time.Now(),
		}

		if err := tokenManager.SaveToken(tokenInfo); err != nil {
			log.Printf("[OAuth] Warning: failed to save token to manager: %v", err)
		} else {
			log.Printf("[OAuth] Token saved to manager with backup")
		}
	}

	if envCount > 0 {
		log.Printf("[OAuth] Saved %d environment variables", envCount)
	}
	if configCount > 0 {
		log.Printf("[OAuth] Saved %d configuration files", configCount)
	}
	if envCount == 0 && configCount == 0 {
		log.Println("[OAuth] Save failed")
	}
}

// getClipboardText gets text from clipboard (platform-specific)
func getClipboardText() (string, error) {
	// プラットフォームに応じたクリップボード取得
	return platformGetClipboard()
}

// setEnvVar sets an environment variable (platform-specific)
func setEnvVar(name, value string) bool {
	// プラットフォームに応じた環境変数設定
	return platformSetEnvVar(name, value)
}
