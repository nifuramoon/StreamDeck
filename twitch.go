package main

import (
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

// --- Profile Image ---

func fetchProf(u string) image.Image {
	if v, ok := profCache.Get(u); ok {
		return v.(image.Image)
	}
	p := filepath.Join(profDir, hsh(u)+".jpg")
	if f, err := os.Open(p); err == nil {
		defer f.Close()
		if img, err := jpeg.Decode(f); err == nil {
			r := resize72(img)
			profCache.Set(u, r)
			return r
		}
	}
	resp, err := httpClient.Get(u)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	os.WriteFile(p, data, 0644)
	img, _ := jpeg.Decode(bytes.NewReader(data))
	if img == nil {
		img, _ = png.Decode(bytes.NewReader(data))
	}
	if img != nil {
		r := resize72(img)
		profCache.Set(u, r)
		return r
	}
	return nil
}

// --- Twitch API ---

func twitchGet(u string, params url.Values) map[string]interface{} {
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
		logAPIError(u, 0, fmt.Sprintf("Network error: %v", err))
		return map[string]interface{}{}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		if newToken := refreshToken(); newToken != "" {
			req.Header.Set("Authorization", "Bearer "+newToken)
			resp, err = httpClient.Do(req)
			if err != nil {
				return map[string]interface{}{}
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				showTokenError("API request failed after token refresh")
				return map[string]interface{}{}
			}
		} else {
			showTokenError("Access token invalid or expired. Please re-authenticate.")
			return map[string]interface{}{}
		}
	} else if resp.StatusCode >= 400 {
		logAPIError(u, resp.StatusCode, "API request failed")
		return map[string]interface{}{}
	}

	var res map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&res)
	if res == nil {
		res = map[string]interface{}{}
	}
	return res
}

func refreshToken() string {
	if RT == "" || CS == "" {
		return ""
	}
	req, _ := http.NewRequest("POST", "https://id.twitch.tv/oauth2/token", strings.NewReader(url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {RT},
		"client_id":     {CID},
		"client_secret": {CS},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var res map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&res)
	newToken, ok := res["access_token"].(string)
	if !ok {
		return ""
	}
	AT = newToken
	if tokenManager != nil && tokenManager.GetCurrentToken() != nil {
		t := tokenManager.GetCurrentToken()
		t.AccessToken = AT
		if newRT, ok := res["refresh_token"].(string); ok {
			t.RefreshToken = newRT
			RT = newRT
		}
		t.ExpiresAt = time.Now().Add(24 * time.Hour)
		tokenManager.SaveToken(t)
	}
	return AT
}

func logAPIError(u string, status int, msg string) {
	log.Printf("[API ERROR] URL: %s, status: %d, reason: %s", u, status, msg)
	if logAnalyzer != nil {
		logAnalyzer.LogAPIError(u, status, msg)
	}
}

// --- Followed Users ---

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
		data, _ := js["data"].([]interface{})
		if len(data) == 0 {
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

// --- Streams ---

func fetchStreams() {
	if CID == "" || AT == "" || len(followed) == 0 {
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
			return
		}
	}

	// Sort by viewer count (bubble sort)
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
	currentOnline := make(map[string]bool)

	for _, s := range online {
		lg := id2lg[fmt.Sprintf("%v", s["user_id"])]
		if lg == "" {
			continue
		}
		currentOnline[lg] = true
		twOrder = append(twOrder, lg)
		views[lg] = int(s["viewer_count"].(float64))

		title := fmt.Sprintf("%v", s["title"])
		titles[lg] = title
		titleW[lg] = float64(measureText(title+"   ", 14))
		titleStep[lg] = (titleW[lg] / math.Max(2.0, minF(8.0, float64(len([]rune(title)))*0.2+1.5))) * SCROLL_IV

		game := fmt.Sprintf("%v", s["game_name"])
		categories[lg] = game
		catW[lg] = float64(measureText(game+"   ", 14))
		catStep[lg] = (catW[lg] / math.Max(2.0, minF(8.0, float64(len([]rune(game)))*0.2+1.5))) * SCROLL_IV

		if t, err := time.Parse(time.RFC3339, fmt.Sprintf("%v", s["started_at"])); err == nil {
			startedAt[lg] = float64(t.Unix())
		}
	}
	stateMu.Unlock()

	// Notifications
	prevOnlineMu.Lock()
	for lg := range currentOnline {
		if !prevOnline[lg] {
			prevOnlineMu.Unlock()
			notifyStreamStart(lg)
			prevOnlineMu.Lock()
		}
	}
	prevOnline = currentOnline
	prevOnlineMu.Unlock()
	go savePrevOnlineState()

	if n := len(online); n != lastOnlineCount {
		if n == 0 {
			log.Println("[API INFO] 現在配信中のフォローユーザーはいません")
		} else {
			log.Printf("[API INFO] 現在 %d 人が配信中です", n)
		}
		lastOnlineCount = n
	}
}

func minF(a, b float64) float64 { if a < b { return a }; return b }

// --- IRC ---

func fetchIRCUsername() bool {
	ircUsernameTries++
	if AT == "" {
		log.Println("[IRC] ユーザー名取得失敗: アクセストークンがありません")
		return false
	}
	log.Printf("[IRC] APIからユーザー名を取得中... (試行 %d)", ircUsernameTries)

	req, _ := http.NewRequest("GET", "https://api.twitch.tv/helix/users", nil)
	req.Header.Set("Client-ID", CID)
	req.Header.Set("Authorization", "Bearer "+AT)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[IRC] ユーザー名取得失敗: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("[IRC] ユーザー名取得エラー: %s", resp.Status)
		if resp.StatusCode == 401 {
			showTokenError("アクセストークンが無効です。OAuthボタンで再認証してください。")
		}
		return false
	}

	var result struct {
		Data []struct {
			Login string `json:"login"`
			ID    string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || len(result.Data) == 0 {
		log.Println("[IRC] ユーザー名取得失敗: レスポンス解析エラーまたはデータなし")
		return false
	}

	ircMu.Lock()
	ircUsername = strings.ToLower(result.Data[0].Login)
	ircUsernameTries = 0
	ircMu.Unlock()

	UID = ircUsername
	os.Setenv("TWITCH_USER_ID", UID)
	saveUsernameToConfig(ircUsername)

	log.Printf("[IRC] ユーザー名取得成功: %s", ircUsername)
	return true
}

func saveUsernameToConfig(username string) {
	configPath := filepath.Join(os.Getenv("HOME"), ".config", "streamdeck-twitch", "config.json")
	configData := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		json.Unmarshal(data, &configData)
	}
	configData["username"] = username
	if data, err := json.MarshalIndent(configData, "", "  "); err == nil {
		os.WriteFile(configPath, data, 0600)
		log.Printf("[CONFIG] ユーザー名を設定ファイルに保存: %s", username)
	}
}

func loadUsernameFromConfig() string {
	configPath := filepath.Join(os.Getenv("HOME"), ".config", "streamdeck-twitch", "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var c map[string]interface{}
		if json.Unmarshal(data, &c); err == nil {
			if u, ok := c["username"].(string); ok {
				return strings.ToLower(u)
			}
		}
	}
	return ""
}

// --- IRC Connection ---

func ircLoop() {
	for {
		if AT == "" {
			time.Sleep(5 * time.Second)
			continue
		}

		// Resolve username
		if ircUsername == "" {
			if u := loadUsernameFromConfig(); u != "" {
				ircUsername = u
			} else if UID != "" {
				ircUsername = strings.ToLower(UID)
			} else if fetchIRCUsername() {
				// success
			} else {
				ircUsername = "justinfan12345"
				log.Printf("[IRC] API取得失敗、匿名ユーザーを使用: %s (送信不可)", ircUsername)
			}
		}

		ircMu.Lock()
		if ircConn == nil {
			if !connectIRC() {
				ircMu.Unlock()
				time.Sleep(5 * time.Second)
				continue
			}
		}

		// Join channel
		if live != "" && !ircJoined[live] {
			log.Printf("[IRC] チャンネルに参加: #%s", live)
			fmt.Fprintf(ircConn, "JOIN #%s\r\n", live)
			ircJoined[live] = true
		}

		// Keepalive
		if time.Since(lastPing) > 2*time.Minute {
			fmt.Fprintf(ircConn, "PING :tmi.twitch.tv\r\n")
			lastPing = time.Now()
		}

		// Read
		ircConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		buf := make([]byte, 4096)
		n, err := ircConn.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				ircMu.Unlock()
				continue
			}
			log.Printf("[IRC] 接続エラー: %v", err)
			ircConn.Close()
			ircConn = nil
			ircJoined = make(map[string]bool)
			ircMu.Unlock()
			time.Sleep(5 * time.Second)
			continue
		}

		// Process lines
		for _, line := range strings.Split(string(buf[:n]), "\r\n") {
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "PING") {
				fmt.Fprintf(ircConn, "PONG :tmi.twitch.tv\r\n")
			} else if strings.Contains(line, "Welcome, GLHF!") {
				log.Println("[IRC] Twitch IRCに正常に接続されました")
			}
		}
		ircMu.Unlock()
	}
}

func connectIRC() bool {
	if AT == "" || ircUsername == "" {
		return false
	}

	c, err := net.Dial("tcp", "irc.chat.twitch.tv:6667")
	if err != nil {
		log.Printf("[IRC] 接続失敗: %v", err)
		return false
	}

	log.Println("[IRC] Twitch IRCに接続しました")
	fmt.Fprintf(c, "PASS oauth:%s\r\n", AT)
	fmt.Fprintf(c, "NICK %s\r\n", ircUsername)
	fmt.Fprintf(c, "CAP REQ :twitch.tv/tags twitch.tv/commands twitch.tv/membership\r\n")

	ircConn = c
	ircJoined = make(map[string]bool)
	return true
}

// --- Chat Sending ---

func ircSend(ch, msg string) {
	log.Printf("[CHAT] チャット送信試行: #%s -> %s", ch, msg)
	sendViaIRC(ch, msg)
}

func sendViaIRC(channel, message string) {
	ircMu.Lock()
	defer ircMu.Unlock()

	if AT == "" {
		log.Printf("[IRC SEND ERROR] アクセストークンがありません")
		return
	}
	if ircConn == nil {
		log.Printf("[IRC SEND ERROR] IRC接続がありません")
		return
	}

	// Scope check
	if !strings.Contains(SCOPE, "user:write:chat") && !strings.Contains(SCOPE, "chat:edit") {
		log.Printf("[IRC SEND ERROR] スコープ不足: チャット送信には user:write:chat または chat:edit が必要")
		if page == LV || page == TX || page == NX {
			showTokenError("チャット送信には追加の権限が必要です。OAuthで再認証してください。")
		}
		return
	}

	// Username check
	if strings.HasPrefix(ircUsername, "justinfan") {
		log.Printf("[IRC SEND ERROR] 匿名ユーザーでは送信できません")
		if fetchIRCUsername() && !strings.HasPrefix(ircUsername, "justinfan") {
			// retry with new username
		} else {
			return
		}
	}

	// Join and send
	if !ircJoined[channel] {
		fmt.Fprintf(ircConn, "JOIN #%s\r\n", channel)
		ircJoined[channel] = true
		time.Sleep(200 * time.Millisecond)
	}

	if n, err := fmt.Fprintf(ircConn, "PRIVMSG #%s :%s\r\n", channel, message); err != nil {
		log.Printf("[IRC SEND ERROR] 送信失敗: %v", err)
		if strings.Contains(err.Error(), "broken pipe") || strings.Contains(err.Error(), "connection reset") {
			ircConn.Close()
			ircConn = nil
		}
	} else {
		log.Printf("[IRC SEND SUCCESS] 送信完了: #%s -> %s (%d bytes)", channel, message, n)
	}
}

// --- User Info Helpers ---

func apiGetUsers(login string) ([]struct {
	ID    string `json:"id"`
	Login string `json:"login"`
}, error) {
	u := "https://api.twitch.tv/helix/users"
	if login != "" {
		u += "?login=" + login
	}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Client-ID", CID)
	req.Header.Set("Authorization", "Bearer "+AT)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API error: %s", resp.Status)
	}

	var result struct {
		Data []struct {
			ID    string `json:"id"`
			Login string `json:"login"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

func getChannelID(username string) (string, error) {
	if username == "" {
		return "", fmt.Errorf("ユーザー名が空です")
	}
	if cached, ok := id2lg[username]; ok {
		return cached, nil
	}
	data, err := apiGetUsers(username)
	if err != nil || len(data) == 0 {
		return "", fmt.Errorf("ユーザーが見つかりません: %s", username)
	}
	id2lg[username] = data[0].ID
	return data[0].ID, nil
}

func getBroadcasterID() (string, error) {
	if UID != "" {
		return UID, nil
	}
	data, err := apiGetUsers("")
	if err != nil || len(data) == 0 {
		return "", fmt.Errorf("ユーザー情報が取得できません")
	}
	UID = data[0].ID
	os.Setenv("TWITCH_USER_ID", UID)
	return UID, nil
}

// --- Main Loops ---

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
		// Auto-return to TW after idle
		if page != TW && page != LV && page != TX && page != NX && now.Sub(lastInput).Seconds() > IDLE_TIMEOUT {
			show(TW, "", false)
		}
		if now.Sub(lastScroll).Seconds() > SCROLL_IV {
			lastScroll = now
			scrollAll()
			if page == TW {
				renderTW()
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func scrollAll() {
	stateMu.Lock()
	defer stateMu.Unlock()

	fields := []struct {
		mode     string
		ofs      map[string]float64
		step     map[string]float64
		width    map[string]float64
		wrapped  map[string]bool
		nextMode string
	}{
		{"title", titleOfs, titleStep, titleW, titleWrapped, "category"},
		{"category", catOfs, catStep, catW, catWrapped, "title"},
	}

	for _, f := range fields {
		if scrollMode != f.mode {
			continue
		}
		allDone := true
		for _, lg := range twOrder {
			f.ofs[lg] += f.step[lg]
			if f.width[lg] > 0 && math.Mod(f.ofs[lg], f.width[lg])+f.step[lg] >= f.width[lg] {
				f.wrapped[lg] = true
			}
			if !f.wrapped[lg] {
				allDone = false
			}
		}
		if allDone && len(twOrder) > 0 {
			scrollMode = f.nextMode
			for _, lg := range twOrder {
				switch f.nextMode {
				case "category":
					catOfs[lg] = 0
					catWrapped[lg] = false
				case "title":
					titleOfs[lg] = 0
					titleWrapped[lg] = false
				}
			}
		}
		break
	}
}

// --- Cache & Config ---

func loadFollowedFromCache() []string {
	data, err := os.ReadFile(followCachePath)
	if err != nil {
		return nil
	}
	var followed []string
	if json.Unmarshal(data, &followed); err != nil {
		return nil
	}
	log.Printf("[Cache] フォローリストをキャッシュから読み込みました (%d人)", len(followed))
	return followed
}

func saveFollowedToCache(followed []string) {
	data, err := json.Marshal(followed)
	if err != nil {
		log.Printf("[Cache] キャッシュ保存エラー: %v", err)
		return
	}
	if err := os.WriteFile(followCachePath, data, 0644); err != nil {
		log.Printf("[Cache] キャッシュ保存エラー: %v", err)
		return
	}
	log.Printf("[Cache] フォローリストをキャッシュに保存しました (%d人)", len(followed))
}

func cachePath(name string) string {
	userCache, _ := os.UserCacheDir()
	return filepath.Join(userCache, "streamdeck-twitch", name)
}

func loadPrevOnlineState() map[string]bool {
	data, err := os.ReadFile(cachePath("prev_online.json"))
	if err != nil {
		return make(map[string]bool)
	}
	var state map[string]bool
	if json.Unmarshal(data, &state); err != nil {
		return make(map[string]bool)
	}
	log.Printf("[Notification] 前回の配信状態を読み込みました: %d人", len(state))
	return state
}

func savePrevOnlineState() {
	prevOnlineMu.RLock()
	defer prevOnlineMu.RUnlock()

	os.MkdirAll(filepath.Dir(cachePath("prev_online.json")), 0755)
	data, _ := json.Marshal(prevOnline)
	os.WriteFile(cachePath("prev_online.json"), data, 0644)
	log.Printf("[Notification] 配信状態を保存しました: %d人", len(prevOnline))
}

func loadNotificationSetting() bool {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return true // default enabled
	}
	var config Config
	if json.Unmarshal(data, &config); err != nil {
		return true
	}
	log.Printf("[Notification] 通知設定: %v", config.NotificationsEnabled)
	return config.NotificationsEnabled
}

// --- Notifications ---

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
	log.Printf("[Notification] %sさんが配信開始", displayName)
	platformSpeakText(displayName + "さんが配信開始")
}

// speakText removed - call platformSpeakText directly