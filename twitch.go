package main


import (
	//
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

)


func fetchProf(u string) image.Image {
	if v, ok := profCache.Get(u); ok {
		return v.(image.Image)
	}
	p := filepath.Join(profDir, hsh(u)+".jpg")
	if f, err := os.Open(p); err == nil {
		defer f.Close()
		if img, err := jpeg.Decode(f); err == nil {
			resized := resize72(img)
			profCache.Set(u, resized)
			return resized
		}
	}
	resp, err := httpClient.Get(u)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	os.WriteFile(p, data, 0644)
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		img, err = png.Decode(bytes.NewReader(data))
	}
	if err == nil {
		resized := resize72(img)
		profCache.Set(u, resized)
		return resized
	}
	return nil
}

func twitchGet(u string, params url.Values) map[string]interface{} {
	// Check if environment variables are set
	if CID == "" || AT == "" {
		debugLog("Environment variables not set, skipping API call: %s", u)
		showTokenError("Missing Client ID or Access Token")
		return map[string]interface{}{}
	}

	if params != nil {
		u += "?" + params.Encode()
	}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Client-ID", CID)
	req.Header.Set("Authorization", "Bearer "+AT)
	resp, err := httpClient.Do(req)

	if err != nil {
		log.Printf("[API ERROR] Network error URL: %s, reason: %v", u, err)
		if logAnalyzer != nil {
			logAnalyzer.LogAPIError(u, 0, fmt.Sprintf("Network error: %v", err))
		}
		return map[string]interface{}{}
	}
	defer resp.Body.Close()

	// Handle token errors
	if resp.StatusCode == 401 {
		warnLog("Token expired or invalid (HTTP 401)")
		if logAnalyzer != nil {
			logAnalyzer.LogAPIError(u, 401, "Token expired or invalid")
		}

		// Try to refresh token if refresh token is available
		if RT != "" && CS != "" {
			infoLog("Attempting token refresh...")
			refReq, _ := http.NewRequest("POST", "https://id.twitch.tv/oauth2/token", strings.NewReader(url.Values{
				"grant_type":    {"refresh_token"},
				"refresh_token": {RT},
				"client_id":     {CID},
				"client_secret": {CS},
			}.Encode()))
			refReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			refResp, err := httpClient.Do(refReq)
			if err != nil {
				log.Printf("[API ERROR] Refresh request failed: %v", err)
				showTokenError("Token refresh failed. Please re-authenticate.")
			} else {
				defer refResp.Body.Close()
				var refRes map[string]interface{}
				json.NewDecoder(refResp.Body).Decode(&refRes)

				if newToken, ok := refRes["access_token"].(string); ok {
					AT = newToken
					log.Println("[API INFO] Token refresh successful!")

					// Update token in token manager
					if tokenManager != nil && tokenManager.GetCurrentToken() != nil {
						token := tokenManager.GetCurrentToken()
						token.AccessToken = AT
						if newRefresh, ok := refRes["refresh_token"].(string); ok {
							token.RefreshToken = newRefresh
							RT = newRefresh
						}
						token.ExpiresAt = time.Now().Add(24 * time.Hour)
						tokenManager.SaveToken(token)
					}

					// Retry the original request
					req.Header.Set("Authorization", "Bearer "+AT)
					retryResp, err := httpClient.Do(req)
					if err != nil {
						log.Printf("[API ERROR] Retry after refresh failed: %v", err)
						return map[string]interface{}{}
					}
					defer retryResp.Body.Close()
					if retryResp.StatusCode >= 400 {
						showTokenError("API request failed after token refresh")
						return map[string]interface{}{}
					}
					var retryRes map[string]interface{}
					json.NewDecoder(retryResp.Body).Decode(&retryRes)
					return retryRes
				}
			}
		}
		// If refresh failed or not available, show error
		showTokenError("Access token invalid or expired. Please re-authenticate.")
		return map[string]interface{}{}
	} else if resp.StatusCode >= 400 {
		log.Printf("[API ERROR] Request failed with status: %d", resp.StatusCode)
		if logAnalyzer != nil {
			logAnalyzer.LogAPIError(u, resp.StatusCode, "API request failed")
		}
		return map[string]interface{}{}
	}

	var res map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&res)
	if res == nil {
		res = map[string]interface{}{}
	}
	return res
}

func fetchFollowedFromAPI() []string {
	if CID == "" || AT == "" || UID == "" {
		log.Println("[API INFO] 環境変数が不足しているためフォローリストの取得をスキップ")
		return nil
	}
	var out []string
	cursor := ""
	for {
		params := url.Values{"user_id": {UID}, "first": {"100"}}
		if cursor != "" {
			params.Set("after", cursor)
		}
		js := twitchGet("https://api.twitch.tv/helix/channels/followed", params)
		data, ok := js["data"].([]interface{})
		if !ok || len(data) == 0 {
			break
		}
		for _, item := range data {
			m, _ := item.(map[string]interface{})
			lg := strings.ToLower(fmt.Sprintf("%v", m["broadcaster_login"]))
			if lg != "" {
				out = append(out, lg)
			}
		}
		pag, _ := js["pagination"].(map[string]interface{})
		cursor, _ = pag["cursor"].(string)
		if cursor == "" {
			break
		}
	}
	return out
}

func fetchUsers(logins []string) {
	for i := 0; i < len(logins); i += 100 {
		end := min(i+100, len(logins))
		params := url.Values{}
		for _, l := range logins[i:end] {
			params.Add("login", l)
		}
		js := twitchGet("https://api.twitch.tv/helix/users", params)
		if data, ok := js["data"].([]interface{}); ok {
			for _, item := range data {
				u := item.(map[string]interface{})
				lg := strings.ToLower(fmt.Sprintf("%v", u["login"]))
				stateMu.Lock()
				lu[lg] = u
				id2lg[fmt.Sprintf("%v", u["id"])] = lg
				stateMu.Unlock()
			}
		}
	}
}

func fetchStreams() {
	if CID == "" || AT == "" {
		log.Println("[API INFO] 環境変数が設定されていないため配信情報の取得をスキップ")
		return
	}
	if len(followed) == 0 {
		return
	}
	stateMu.RLock()
	var uids []string
	for _, f := range followed {
		if u, ok := lu[f]; ok {
			uids = append(uids, fmt.Sprintf("%v", u["id"]))
		}
	}
	stateMu.RUnlock()

	var online []map[string]interface{}
	hasError := false

	for i := 0; i < len(uids); i += 100 {
		end := min(i+100, len(uids))
		params := url.Values{}
		for _, uid := range uids[i:end] {
			params.Add("user_id", uid)
		}
		js := twitchGet("https://api.twitch.tv/helix/streams", params)

		if data, ok := js["data"].([]interface{}); ok {
			for _, item := range data {
				online = append(online, item.(map[string]interface{}))
			}
		} else {
			hasError = true
			break
		}
	}

	if hasError {
		return
	}

	for i := 0; i < len(online); i++ {
		for j := i + 1; j < len(online); j++ {
			if online[j]["viewer_count"].(float64) > online[i]["viewer_count"].(float64) {
				online[i], online[j] = online[j], online[i]
			}
		}
	}
	if len(online) > MAX_TWITCH_KEYS {
		online = online[:MAX_TWITCH_KEYS]
	}

	stateMu.Lock()
	twOrder = nil
	views = map[string]int{}
	startedAt = map[string]float64{}

	// 現在オンラインの配信者を記録
	currentOnlineMap := make(map[string]bool)
	for _, s := range online {
		lg := id2lg[fmt.Sprintf("%v", s["user_id"])]
		if lg == "" {
			continue
		}
		currentOnlineMap[lg] = true
		twOrder = append(twOrder, lg)
		views[lg] = int(s["viewer_count"].(float64))
		title := fmt.Sprintf("%v", s["title"])
		titles[lg] = title
		titleStep[lg] = (float64(measureText(title+"   ", 14)) / math.Max(2.0, minF(8.0, float64(len([]rune(title)))*0.2+1.5))) * SCROLL_IV
		titleW[lg] = float64(measureText(title+"   ", 14))
		game := fmt.Sprintf("%v", s["game_name"])
		categories[lg] = game
		catStep[lg] = (float64(measureText(game+"   ", 14)) / math.Max(2.0, minF(8.0, float64(len([]rune(game)))*0.2+1.5))) * SCROLL_IV
		catW[lg] = float64(measureText(game+"   ", 14))
		if t, err := time.Parse(time.RFC3339, fmt.Sprintf("%v", s["started_at"])); err == nil {
			startedAt[lg] = float64(t.Unix())
		}
	}
	stateMu.Unlock()

	// 配信開始通知のチェック
	prevOnlineMu.Lock()
	for lg := range currentOnlineMap {
		if !prevOnline[lg] {
			prevOnlineMu.Unlock()
			notifyStreamStart(lg)
			prevOnlineMu.Lock()
		}
	}
	prevOnline = currentOnlineMap
	prevOnlineMu.Unlock()

	go savePrevOnlineState()

	currentOnline := len(online)
	if currentOnline != lastOnlineCount {
		if currentOnline == 0 {
			log.Println("[API INFO] 現在配信中のフォローユーザーはいません (0人オンライン)")
		} else {
			log.Printf("[API INFO] 現在 %d 人が配信中です\n", currentOnline)
		}
		lastOnlineCount = currentOnline
	}
}
func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// --- Renderers ---
func fetchIRCUsername() bool {
	ircUsernameTries++

	if AT == "" {
		log.Println("[IRC] ユーザー名取得失敗: アクセストークンがありません")
		return false
	}

	log.Printf("[IRC] APIからユーザー名を取得中... (試行 %d)", ircUsernameTries)

	// スコープチェック（ユーザー情報取得に必要なスコープ）
	requiredScopes := []string{"user:read:email", "user:read"}
	hasScope := false
	for _, scope := range requiredScopes {
		if strings.Contains(SCOPE, scope) {
			hasScope = true
			break
		}
	}

	if !hasScope {
		log.Printf("[IRC WARN] スコープ不足の可能性: 現在のスコープ: %s", SCOPE)
		log.Printf("[IRC WARN] ユーザー情報取得には以下のいずれかが必要: %v", requiredScopes)
		// 続行（既存のトークンで試す）
	}

	req, err := http.NewRequest("GET", "https://api.twitch.tv/helix/users", nil)
	if err != nil {
		log.Printf("[IRC] ユーザー名取得リクエスト作成失敗: %v", err)
		return false
	}

	req.Header.Set("Client-ID", CID)
	req.Header.Set("Authorization", "Bearer "+AT)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[IRC] ユーザー名取得失敗: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("[IRC] ユーザー名取得エラー: %s", resp.Status)

		// トークンが無効な場合
		if resp.StatusCode == 401 {
			log.Println("[IRC] アクセストークンが無効です。再認証が必要です。")
			showTokenError("アクセストークンが無効です。OAuthボタンで再認証してください。")
		}
		return false
	}

	var result struct {
		Data []struct {
			ID              string `json:"id"`
			Login           string `json:"login"`
			DisplayName     string `json:"display_name"`
			BroadcasterType string `json:"broadcaster_type"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[IRC] ユーザー名レスポンス解析失敗: %v", err)
		return false
	}

	if len(result.Data) == 0 {
		log.Println("[IRC] ユーザー名取得失敗: ユーザーデータがありません")
		return false
	}

	ircMu.Lock()
	ircUsername = strings.ToLower(result.Data[0].Login)
	ircUsernameTries = 0 // 成功したらリセット
	ircMu.Unlock()

	// 環境変数とグローバル変数を更新
	os.Setenv("TWITCH_USER_ID", ircUsername)
	UID = ircUsername

	// 設定ファイルにも保存
	saveUsernameToConfig(ircUsername)

	log.Printf("[IRC] ユーザー名取得成功: %s (ID: %s)", ircUsername, result.Data[0].ID)
	return true
}

// saveUsernameToConfig saves the username to config file
func saveUsernameToConfig(username string) {
	configPath := filepath.Join(os.Getenv("HOME"), ".config", "streamdeck-twitch", "config.json")

	// 既存の設定を読み込み
	configData := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		json.Unmarshal(data, &configData)
	}

	// ユーザー名を追加/更新
	configData["username"] = username

	// 保存
	if data, err := json.MarshalIndent(configData, "", "  "); err == nil {
		os.WriteFile(configPath, data, 0600)
		log.Printf("[CONFIG] ユーザー名を設定ファイルに保存: %s", username)
	}
}

// loadUsernameFromConfig loads username from config file
func loadUsernameFromConfig() string {
	configPath := filepath.Join(os.Getenv("HOME"), ".config", "streamdeck-twitch", "config.json")

	if data, err := os.ReadFile(configPath); err == nil {
		var configData map[string]interface{}
		if err := json.Unmarshal(data, &configData); err == nil {
			if username, ok := configData["username"].(string); ok {
				return strings.ToLower(username)
			}
		}
	}
	return ""
}

func ircLoop() {
	for {
		// アクセストークンがない場合は接続を試みない
		if AT == "" {
			time.Sleep(5 * time.Second)
			continue
		}

		// ユーザー名を確実に取得（初回のみ）
		if ircUsername == "" {
			usernameFromConfig := loadUsernameFromConfig()

			if usernameFromConfig != "" {
				// 1. 設定ファイルから読み込み
				ircUsername = usernameFromConfig
				log.Printf("[IRC] 設定ファイルからユーザー名読み込み: %s", ircUsername)
			} else if UID != "" {
				// 2. 環境変数から
				ircUsername = strings.ToLower(UID)
				log.Printf("[IRC] 環境変数からユーザー名設定: %s", ircUsername)
			} else if AT != "" {
				// 3. APIから取得（同期的に）
				log.Println("[IRC] APIからユーザー名を取得します...")
				if fetchIRCUsername() {
					log.Printf("[IRC] APIからユーザー名取得成功: %s", ircUsername)
				} else {
					// API取得失敗時は匿名ユーザーを使用（読み取り専用）
					ircUsername = "justinfan12345"
					log.Printf("[IRC] API取得失敗、匿名ユーザーを使用: %s (送信不可)", ircUsername)
				}
			} else {
				// 4. デフォルト（最終手段）
				ircUsername = "justinfan12345"
				log.Printf("[IRC] デフォルト匿名ユーザーを使用: %s (送信不可)", ircUsername)
			}
		}

		ircMu.Lock()
		if ircConn == nil {
			debugLog("[IRC DEBUG] IRC接続試行: token=%v, username=%v", AT != "", ircUsername)

			// アクセストークンがない場合は接続しない
			if AT == "" {
				debugLog("[IRC DEBUG] アクセストークンなし、接続スキップ")
				ircMu.Unlock()
				time.Sleep(5 * time.Second)
				continue
			}

			// ユーザー名が未設定の場合は取得を試みる
			if ircUsername == "" || strings.HasPrefix(ircUsername, "justinfan") {
				debugLog("[IRC DEBUG] 有効なユーザー名がありません。取得を試みます...")
				ircMu.Unlock()

				// ユーザー名取得を試みる
				if AT != "" {
					fetchIRCUsername()
				}

				time.Sleep(2 * time.Second)
				continue
			}

			if c, err := net.Dial("tcp", "irc.chat.twitch.tv:6667"); err == nil {
				log.Println("[IRC] Twitch IRCに接続しました")

				// Twitch IRC接続シーケンス
				tokenPreview := "none"
				if len(AT) > 10 {
					tokenPreview = AT[:10] + "..."
				} else if AT != "" {
					tokenPreview = "present"
				}
				debugLog("[IRC DEBUG] 認証送信: PASS oauth:%s", tokenPreview)
				fmt.Fprintf(c, "PASS oauth:%s\r\n", AT)

				// ニックネーム設定
				nick := ircUsername
				// justinfan系ユーザーの場合は読み取り専用モード
				isReadOnly := strings.HasPrefix(nick, "justinfan")
				if isReadOnly {
					debugLog("[IRC DEBUG] 読み取り専用モード: NICK %s", nick)
				} else {
					debugLog("[IRC DEBUG] 送信可能モード: NICK %s", nick)
				}
				fmt.Fprintf(c, "NICK %s\r\n", nick)

				debugLog("[IRC DEBUG] CAPABILITY要求送信")
				fmt.Fprintf(c, "CAP REQ :twitch.tv/tags twitch.tv/commands twitch.tv/membership\r\n")

				ircConn = c
				ircJoined = make(map[string]bool) // 参加済みチャンネルをリセット
				debugLog("[IRC DEBUG] IRC接続初期化完了")
			} else {
				log.Printf("[IRC] 接続失敗: %v", err)
				ircMu.Unlock()
				time.Sleep(5 * time.Second)
				continue
			}
		}

		if ircConn == nil {
			debugLog("[IRC DEBUG] IRC接続試行: token=%v, username=%v", AT != "", ircUsername)
			if c, err := net.Dial("tcp", "irc.chat.twitch.tv:6667"); err == nil {
				log.Println("[IRC] Twitch IRCに接続しました")

				// Twitch IRC接続シーケンス
				debugLog("[IRC DEBUG] 認証送信: PASS oauth:%s", AT[:min(10, len(AT))]+"...")
				fmt.Fprintf(c, "PASS oauth:%s\r\n", AT)

				nick := "justinfan12345"
				if ircUsername != "" {
					nick = ircUsername
				}
				debugLog("[IRC DEBUG] ニックネーム設定: NICK %s", nick)
				fmt.Fprintf(c, "NICK %s\r\n", nick)

				debugLog("[IRC DEBUG] CAPABILITY要求送信")
				fmt.Fprintf(c, "CAP REQ :twitch.tv/tags twitch.tv/commands twitch.tv/membership\r\n")

				ircConn = c
				ircJoined = make(map[string]bool) // 参加済みチャンネルをリセット
				debugLog("[IRC DEBUG] IRC接続初期化完了")
			} else {
				log.Printf("[IRC] 接続失敗: %v", err)
				ircMu.Unlock()
				time.Sleep(5 * time.Second)
				continue
			}
		}

		// 現在のライブチャンネルに参加
		if live != "" && !ircJoined[live] {
			log.Printf("[IRC] チャンネルに参加: #%s", live)
			fmt.Fprintf(ircConn, "JOIN #%s\r\n", live)
			ircJoined[live] = true
			debugLog("[IRC DEBUG] Joined channel: #%s (joined map: %v)", live, ircJoined)
		}

		// 定期的なPING送信（接続維持）
		if time.Since(lastPing) > 2*time.Minute {
			fmt.Fprintf(ircConn, "PING :tmi.twitch.tv\r\n")
			lastPing = time.Now()
			debugLog("[IRC DEBUG] 接続維持のためPING送信")
		}

		// メッセージ受信
		ircConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		buf := make([]byte, 4096)
		n, err := ircConn.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				ircMu.Unlock()
				continue
			}

			// EOFエラーの場合は接続が切断されたと判断
			if err == io.EOF {
				log.Println("[IRC] 接続が切断されました (EOF)。再接続します...")
			} else {
				log.Printf("[IRC] 接続エラー: %v", err)
			}

			// 接続をクリーンアップ
			if ircConn != nil {
				ircConn.Close()
			}
			ircConn = nil
			ircJoined = make(map[string]bool) // 参加済みチャンネルをリセット
			ircMu.Unlock()

			// 再接続前に少し待機
			time.Sleep(5 * time.Second)
			continue
		}

		// 受信データ処理
		data := string(buf[:n])
		lines := strings.Split(data, "\r\n")
		for _, line := range lines {
			if line == "" {
				continue
			}

			// PING応答
			if strings.HasPrefix(line, "PING") {
				fmt.Fprintf(ircConn, "PONG :tmi.twitch.tv\r\n")
				log.Println("[IRC] PINGに応答")
			}

			// 接続確認メッセージ
			if strings.Contains(line, "Welcome, GLHF!") {
				log.Println("[IRC] Twitch IRCに正常に接続されました")
			}
		}
		ircMu.Unlock()
	}
}

func ircSend(ch, msg string) {
	log.Printf("[CHAT] チャット送信試行: #%s -> %s", ch, msg)
	debugLog("[CHAT DEBUG] 現在の状態: AT=%v, CID=%v, ircUsername=%s", AT != "", CID != "", ircUsername)

	// Twitch APIを使用したチャット送信
	sendChatMessage(ch, msg)
}

// sendChatMessage sends a chat message using Twitch Helix API
func sendChatMessage(channel, message string) {
	debugLog("[CHAT DEBUG] sendChatMessage called: channel=%s, message=%s", channel, message)

	if AT == "" || CID == "" {
		log.Printf("[CHAT ERROR] 送信失敗: アクセストークンまたはClient IDがありません")
		debugLog("[CHAT DEBUG] AT empty: %v, CID empty: %v", AT == "", CID == "")
		return
	}

	if channel == "" {
		log.Printf("[CHAT ERROR] 送信失敗: チャンネル名が空です")
		return
	}

	if message == "" {
		log.Printf("[CHAT ERROR] 送信失敗: メッセージが空です")
		return
	}

	// チャンネルIDを取得（ユーザー名から）
	channelID, err := getChannelID(channel)
	if err != nil {
		log.Printf("[CHAT ERROR] チャンネルID取得失敗: %v", err)
		return
	}

	// デバッグログ
	debugLog("[CHAT DEBUG] チャンネルID: %s (for %s)", channelID, channel)

	// ブロードキャスターIDを取得（送信者）
	broadcasterID, err := getBroadcasterID()
	if err != nil {
		log.Printf("[CHAT WARN] ブロードキャスターID取得失敗: %v", err)
		log.Printf("[CHAT INFO] broadcasterIDなしでIRC送信を試みます")
		broadcasterID = "" // 空でも続行
	}

	// Twitch Helix API: POST /helix/chat/messages
	// 注意: このエンドポイントは現在ベータ版で、特別なアクセス権が必要かもしれません
	// 代替として、従来のIRCを使用するか、別の方法を検討

	log.Printf("[CHAT WARN] Twitch Helix chat/messages APIは制限がある可能性があります")
	log.Printf("[CHAT INFO] 代替方法としてIRC送信を試みます")

	// IRCを使用した送信（OAuthトークンとユーザー名が必要）
	debugLog("[CHAT DEBUG] broadcasterID取得結果: %s", broadcasterID)

	// broadcasterIDが空でもIRC送信を試みる
	sendViaIRC(channel, message, broadcasterID)
}

// getChannelID gets the channel ID from username
func getChannelID(username string) (string, error) {
	if username == "" {
		return "", fmt.Errorf("ユーザー名が空です")
	}

	// キャッシュがあれば使用
	if cachedID, ok := id2lg[username]; ok {
		return cachedID, nil
	}

	// APIから取得
	url := fmt.Sprintf("https://api.twitch.tv/helix/users?login=%s", username)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Client-ID", CID)
	req.Header.Set("Authorization", "Bearer "+AT)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("APIエラー: %s", resp.Status)
	}

	var result struct {
		Data []struct {
			ID    string `json:"id"`
			Login string `json:"login"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if len(result.Data) == 0 {
		return "", fmt.Errorf("ユーザーが見つかりません: %s", username)
	}

	// キャッシュに保存
	id2lg[username] = result.Data[0].ID
	return result.Data[0].ID, nil
}

// getBroadcasterID gets the broadcaster ID (current user)
func getBroadcasterID() (string, error) {
	if UID != "" {
		return UID, nil
	}

	// APIから取得
	url := "https://api.twitch.tv/helix/users"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Client-ID", CID)
	req.Header.Set("Authorization", "Bearer "+AT)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("APIエラー: %s", resp.Status)
	}

	var result struct {
		Data []struct {
			ID    string `json:"id"`
			Login string `json:"login"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if len(result.Data) == 0 {
		return "", fmt.Errorf("ユーザー情報が取得できません")
	}

	// 環境変数とグローバル変数を更新
	UID = result.Data[0].ID
	os.Setenv("TWITCH_USER_ID", UID)

	return UID, nil
}

// sendViaIRC sends a message via IRC with proper OAuth authentication
func sendViaIRC(channel, message, broadcasterID string) {
	debugLog("[IRC SEND DEBUG] sendViaIRC called: channel=%s, message=%s", channel, message)

	ircMu.Lock()
	defer ircMu.Unlock()

	log.Printf("[IRC SEND] 送信試行: #%s -> %s", channel, message)

	// 必須チェック
	debugLog("[IRC SEND DEBUG] 必須チェック: AT=%v, ircConn=%v, ircUsername=%s", AT != "", ircConn != nil, ircUsername)
	if AT == "" {
		log.Printf("[IRC SEND ERROR] アクセストークンがありません")
		return
	}

	// スコープチェック（チャット送信に必要なスコープ）
	debugLog("[IRC SEND DEBUG] スコープチェック: 現在のスコープ=%s", SCOPE)
	requiredChatScopes := []string{"user:write:chat", "chat:edit"}
	hasChatScope := false
	for _, scope := range requiredChatScopes {
		if strings.Contains(SCOPE, scope) {
			hasChatScope = true
			debugLog("[IRC SEND DEBUG] 必要なスコープを確認: %s", scope)
			break
		}
	}

	if !hasChatScope {
		log.Printf("[IRC SEND ERROR] スコープ不足: チャット送信には以下のいずれかが必要: %v", requiredChatScopes)
		log.Printf("[IRC SEND ERROR] 現在のスコープ: %s", SCOPE)
		log.Printf("[IRC SEND INFO] OAuth認証をやり直して適切なスコープを取得してください")
		log.Printf("[IRC SEND INFO] 手順: ホーム画面 → OAuthボタン → 認証後、Save envボタン")

		// ユーザーに視覚的なフィードバックを提供
		if page == LV || page == TX || page == NX {
			showTokenError("チャット送信には追加の権限が必要です。OAuthで再認証してください。")
		}
		return
	}

	debugLog("[IRC SEND DEBUG] スコープチェック通過")

	if ircConn == nil {
		log.Printf("[IRC SEND ERROR] IRC接続がありません")
		return
	}

	// ユーザー名がjustinfan系でないことを確認
	debugLog("[IRC SEND DEBUG] ユーザー名チェック: ircUsername=%s", ircUsername)
	if strings.HasPrefix(ircUsername, "justinfan") {
		log.Printf("[IRC SEND ERROR] 匿名ユーザー %s では送信できません", ircUsername)
		log.Printf("[IRC SEND INFO] OAuth認証を行って有効なユーザー名を取得してください")

		// ユーザー名を再取得してみる
		log.Println("[IRC SEND] ユーザー名を再取得します...")
		if fetchIRCUsername() {
			log.Printf("[IRC SEND] ユーザー名再取得成功: %s", ircUsername)
			// 再取得後もjustinfan系なら送信不可
			if strings.HasPrefix(ircUsername, "justinfan") {
				return
			}
		} else {
			return
		}
	}

	debugLog("[IRC SEND DEBUG] ユーザー名チェック通過")

	// チャンネルに参加しているか確認・参加
	if !ircJoined[channel] {
		log.Printf("[IRC SEND] チャンネルに参加: #%s", channel)
		joinCmd := fmt.Sprintf("JOIN #%s\r\n", channel)
		if _, err := fmt.Fprintf(ircConn, joinCmd); err != nil {
			log.Printf("[IRC SEND ERROR] チャンネル参加失敗: %v", err)
			return
		}
		ircJoined[channel] = true
		time.Sleep(200 * time.Millisecond) // 参加処理待ち
	}

	// メッセージ送信
	msgCmd := fmt.Sprintf("PRIVMSG #%s :%s\r\n", channel, message)
	debugLog("[IRC SEND DEBUG] 送信コマンド: %s", strings.TrimSpace(msgCmd))

	n, err := fmt.Fprintf(ircConn, msgCmd)
	if err != nil {
		log.Printf("[IRC SEND ERROR] 送信失敗: %v (bytes: %d)", err, n)

		// 接続エラーの場合は再接続
		if strings.Contains(err.Error(), "broken pipe") ||
			strings.Contains(err.Error(), "connection reset") {
			log.Println("[IRC SEND] 接続エラー、再接続を試みます")
			if ircConn != nil {
				ircConn.Close()
			}
			ircConn = nil
		}
	} else {
		log.Printf("[IRC SEND SUCCESS] 送信完了: #%s -> %s (%d bytes)", channel, message, n)
	}
}

// --- Loops ---
func bgLoop() {
	for {
		fetchStreams()
		time.Sleep(FETCH_IV * time.Second)
	}
}
func mainLoop() {
	lastFetch, lastScroll := time.Now(), time.Now()
	for {
		now := time.Now()
		if now.Sub(lastFetch).Seconds() > FETCH_IV {
			lastFetch = now
			if page == TW {
				renderTW()
			}
		}
		// コメント窓以外で1分以上の無操作状態の場合TWITCH窓へ移行
		if page != TW && page != LV && page != TX && page != NX && now.Sub(lastInput).Seconds() > IDLE_TIMEOUT {
			show(TW, "", false)
		}
		if now.Sub(lastScroll).Seconds() > SCROLL_IV {
			lastScroll = now
			stateMu.Lock()
			if scrollMode == "title" {
				allDone := true
				for _, lg := range twOrder {
					titleOfs[lg] += titleStep[lg]
					if titleW[lg] > 0 && math.Mod(titleOfs[lg], titleW[lg])+titleStep[lg] >= titleW[lg] {
						titleWrapped[lg] = true
					}
					if !titleWrapped[lg] {
						allDone = false
					}
				}
				if allDone && len(twOrder) > 0 {
					scrollMode = "category"
					for _, lg := range twOrder {
						catOfs[lg] = 0
						catWrapped[lg] = false
					}
				}
			} else {
				allDone := true
				for _, lg := range twOrder {
					catOfs[lg] += catStep[lg]
					if catW[lg] > 0 && math.Mod(catOfs[lg], catW[lg])+catStep[lg] >= catW[lg] {
						catWrapped[lg] = true
					}
					if !catWrapped[lg] {
						allDone = false
					}
				}
				if allDone && len(twOrder) > 0 {
					scrollMode = "title"
					for _, lg := range twOrder {
						titleOfs[lg] = 0
						titleWrapped[lg] = false
					}
				}
			}
			stateMu.Unlock()
			if page == TW {
				renderTW()
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func loadFollowedFromCache() []string {
	data, err := os.ReadFile(followCachePath)
	if err != nil {
		return nil
	}

	var followed []string
	if err := json.Unmarshal(data, &followed); err != nil {
		return nil
	}

	log.Printf("[Cache] フォローリストをキャッシュから読み込みました (%d人)", len(followed))
	return followed
}

func saveFollowedToCache(followed []string) {
	data, err := json.Marshal(followed)
	if err != nil {
		log.Printf("[Cache] フォローリストのキャッシュ保存エラー: %v", err)
		return
	}

	if err := os.WriteFile(followCachePath, data, 0644); err != nil {
		log.Printf("[Cache] フォローリストのキャッシュ保存エラー: %v", err)
		return
	}

	log.Printf("[Cache] フォローリストをキャッシュに保存しました (%d人)", len(followed))
}

// loadPrevOnlineState loads previous online state from file
func loadPrevOnlineState() map[string]bool {
	userCache, err := os.UserCacheDir()
	if err != nil {
		return make(map[string]bool)
	}

	statePath := filepath.Join(userCache, "streamdeck-twitch", "prev_online.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		return make(map[string]bool)
	}

	var state map[string]bool
	if err := json.Unmarshal(data, &state); err != nil {
		return make(map[string]bool)
	}

	log.Printf("[Notification] 前回の配信状態を読み込みました: %d人", len(state))
	return state
}

// savePrevOnlineState saves current online state to file
func savePrevOnlineState() {
	prevOnlineMu.RLock()
	defer prevOnlineMu.RUnlock()

	userCache, err := os.UserCacheDir()
	if err != nil {
		return
	}

	cacheDir := filepath.Join(userCache, "streamdeck-twitch")
	os.MkdirAll(cacheDir, 0755)

	statePath := filepath.Join(cacheDir, "prev_online.json")
	data, err := json.Marshal(prevOnline)
	if err != nil {
		return
	}

	os.WriteFile(statePath, data, 0644)
	log.Printf("[Notification] 配信状態を保存しました: %d人", len(prevOnline))
}

// loadNotificationSetting loads notification setting from config file
func loadNotificationSetting() bool {
	initConfig()

	data, err := os.ReadFile(configPath)
	if err != nil {
		return true // デフォルトは有効
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return true // デフォルトは有効
	}

	log.Printf("[Notification] 通知設定を読み込みました: %v", config.NotificationsEnabled)
	return config.NotificationsEnabled
}

// --- Stream Notification Functions ---

// notifyStreamStart handles stream start notifications
func notifyStreamStart(login string) {
	stateMu.RLock()
	userInfo, ok := lu[login]
	stateMu.RUnlock()

	if !ok {
		log.Printf("[Notification] ユーザー情報が見つかりません: %s", login)
		return
	}

	displayName := fmt.Sprintf("%v", userInfo["display_name"])
	if displayName == "" {
		displayName = login
	}

	message := fmt.Sprintf("%sさんが配信開始", displayName)
	log.Printf("[Notification] %s", message)

	// 音声通知を実行
	speakText(message)
}

// speakText uses platform-specific TTS to speak text
func speakText(text string) {
	log.Printf("[TTS] 音声合成: %s", text)

	go func() {
		platformSpeakText(text)
	}()
}

