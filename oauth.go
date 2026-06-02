package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
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

	// OAuth callback server management
	oauthServer   *http.Server
	oauthServerMu sync.Mutex
	oauthDone     = make(chan bool, 1)
	oauthResult   = ""
)

// startOAuth starts the OAuth flow by opening browser and auto-capturing the callback
func startOAuth() {
	clientID := CID
	clientSecret := CS

	select {
	case <-oauthDone:
	default:
	}

	log.Printf("[OAuth Debug] CID='%s', CS='%s', SCOPE='%s'", CID, CS, SCOPE)
	log.Printf("[OAuth Debug] tempClientID='%s', tempClientSecret='%s'", tempClientID, tempClientSecret)

	if clientID == "" {
		clientID = tempClientID
	}
	if clientSecret == "" {
		clientSecret = tempClientSecret
	}

	if clientID == "" {
		log.Println("[OAuth] ERROR: Client ID not set")
		return
	}

	scope := SCOPE
	if scope == "" {
		scope = oauthScope
	}
	scope = ensureCompleteScope(scope)

	// Start local HTTP server to auto-capture OAuth callback
	startOAuthCallbackServer()

	authURL := fmt.Sprintf(
		"https://id.twitch.tv/oauth2/authorize?client_id=%s&redirect_uri=http://localhost:8080&response_type=code&scope=%s",
		clientID, url.QueryEscape(scope))

	log.Printf("[OAuth] Opening browser: %s", authURL)
	platformOpenBrowser(authURL)

	// Wait for callback in background
	go waitForOAuthCallback()
}

// startOAuthCallbackServer starts a local HTTP server to receive the OAuth callback
func startOAuthCallbackServer() {
	oauthServerMu.Lock()
	defer oauthServerMu.Unlock()

	if oauthServer != nil {
		log.Println("[OAuth] Callback server already running")
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", oauthCallbackHandler)

	oauthServer = &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	go func() {
		log.Println("[OAuth] Callback server started on http://localhost:8080")
		if err := oauthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[OAuth] Callback server error: %v", err)
		}
	}()
}

// oauthCallbackHandler handles the OAuth redirect callback from Twitch
func oauthCallbackHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	errorParam := r.URL.Query().Get("error")
	errorDesc := r.URL.Query().Get("error_description")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if errorParam != "" {
		log.Printf("[OAuth] Authorization denied: %s - %s", errorParam, errorDesc)
		oauthResult = fmt.Sprintf("認証拒否: %s", errorDesc)
		oauthDone <- false
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>StreamDeck OAuth</title><style>body{font-family:sans-serif;text-align:center;padding:50px;background:#1a1a2e;color:#eee}h1{font-size:24px}.err{color:#ff6b6b}.info{color:#aaa;margin-top:20px}</style></head><body><h1 class="err">認証失敗</h1><p>%s</p><p class="info">このウィンドウは閉じてください</p></body></html>`, errorDesc)
		go stopOAuthCallbackServer()
		return
	}

	if code == "" {
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>StreamDeck OAuth</title><style>body{font-family:sans-serif;text-align:center;padding:50px;background:#1a1a2e;color:#eee}h1{font-size:24px}.err{color:#ff6b6b}</style></head><body><h1 class="err">エラー</h1><p>認可コードがありません</p></body></html>`)
		return
	}

	log.Printf("[OAuth] Authorization code received via callback")

	// Return success page immediately, then process token exchange in background
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>StreamDeck OAuth</title><style>body{font-family:sans-serif;text-align:center;padding:50px;background:#1a1a2e;color:#eee}h1{font-size:24px;color:#4ecdc4}.info{color:#aaa;margin-top:20px}</style></head><body><h1>認証成功</h1><p>StreamDeckがトークンを取得中です...</p><p class="info">このウィンドウは閉じてください</p></body></html>`)

	go exchangeCodeForTokens(code)
	go stopOAuthCallbackServer()
}

// waitForOAuthCallback waits for the OAuth callback result and handles it on the main goroutine
func waitForOAuthCallback() {
	select {
	case success := <-oauthDone:
		if success {
			log.Println("[OAuth] 自動認証完了！")
			// Update Stream Deck display
			updateAfterOAuthSuccess()
		} else {
			log.Printf("[OAuth] 自動認証失敗: %s", oauthResult)
		}
	case <-time.After(5 * time.Minute):
		log.Println("[OAuth] Callback timeout (5 minutes)")
		stopOAuthCallbackServer()
	}
}

// updateAfterOAuthSuccess updates the app state after successful OAuth
func updateAfterOAuthSuccess() {
	// Update globals
	AT = oauthAccessToken
	RT = oauthRefreshToken
	UID = oauthUserID

	// Save immediately
	saveEnvVars()

	// Refresh display
	if page == OA {
		show(TW, "", false)
	}

	// Re-fetch data
	go func() {
		apiFollows := fetchFollowedFromAPI()
		if len(apiFollows) > 0 {
			followed = apiFollows
			saveFollowedToCache(apiFollows)
			fetchUsers(apiFollows)
		}
		fetchStreams()
		renderTW()
	}()

	log.Printf("[OAuth] Successfully authenticated as user: %s", oauthLoginName)
}

// stopOAuthCallbackServer gracefully stops the OAuth callback server
func stopOAuthCallbackServer() {
	oauthServerMu.Lock()
	defer oauthServerMu.Unlock()

	if oauthServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		oauthServer.Shutdown(ctx)
		oauthServer = nil
		log.Println("[OAuth] Callback server stopped")
	}
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
	clientID := CID
	clientSecret := CS

	if clientID == "" {
		clientID = tempClientID
	}
	if clientSecret == "" {
		clientSecret = tempClientSecret
	}

	if clientID == "" || clientSecret == "" {
		log.Println("[OAuth] エラー: Client IDまたはClient Secretが設定されていません")
		oauthResult = "Client ID/Secret 未設定"
		oauthDone <- false
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
		oauthResult = fmt.Sprintf("ネットワークエラー: %v", err)
		oauthDone <- false
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

		AT = oauthAccessToken
		RT = oauthRefreshToken

		getUserInfo()
		oauthDone <- true
	} else {
		log.Printf("[OAuth] エラー: %d - %s", resp.StatusCode, string(body))
		errBody := string(body)
		if strings.Contains(errBody, "invalid client secret") {
			oauthResult = "Client Secretが無効です。config.jsonのTWITCH_CLIENT_SECRETを確認してください"
			log.Printf("[OAuth] %s", oauthResult)
		} else if strings.Contains(errBody, "invalid") {
			oauthResult = fmt.Sprintf("認証情報無効: %s", errBody)
		} else {
			oauthResult = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, errBody)
		}
		oauthDone <- false
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
		isDefaultCID := saveClientID == ""
		isDefaultCS := saveClientSecret == ""

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

		if saveClientID != "" && existingConfig.ClientID != saveClientID {
			existingConfig.ClientID = saveClientID
			updated = true
			log.Println("[OAuth] Updated Client ID in config")
		}

		if saveClientSecret != "" && existingConfig.ClientSecret != saveClientSecret {
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

	if configCount > 0 {
		log.Printf("[OAuth] Saved %d configuration files", configCount)
	} else {
		log.Println("[OAuth] No config changes to save")
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
