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
	"time"
)

var (
	oauthServer *http.Server
	oauthDone   = make(chan bool, 1)
	oauthResult string
)

func clientID() string {
	if CID != "" {
		return CID
	}
	return tempClientID
}

func clientSecret() string {
	if CS != "" {
		return CS
	}
	return tempClientSecret
}

func startOAuth() {
	cid, cs := clientID(), clientSecret()
	if cid == "" {
		log.Println("[OAuth] ERROR: Client ID not set")
		return
	}

	scope := SCOPE
	if scope == "" {
		scope = "user:read:email user:read:follows user:read:broadcast"
	}
	scope = ensureScope(scope)

	startOAuthServer()
	authURL := fmt.Sprintf("https://id.twitch.tv/oauth2/authorize?client_id=%s&redirect_uri=http://localhost:8080&response_type=code&scope=%s",
		cid, url.QueryEscape(scope))
	log.Printf("[OAuth] Opening: %s", authURL)
	platformOpenBrowser(authURL)

	go waitForCallback()
}

func startOAuthServer() {
	if oauthServer != nil {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", oauthCallback)
	oauthServer = &http.Server{Addr: ":8080", Handler: mux}
	go func() {
		log.Println("[OAuth] Server on :8080")
		if err := oauthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[OAuth] Server error: %v", err)
		}
	}()
}

func oauthCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	code := r.URL.Query().Get("code")
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		desc := r.URL.Query().Get("error_description")
		log.Printf("[OAuth] Denied: %s - %s", errParam, desc)
		oauthResult = errParam
		oauthDone <- false
		fmt.Fprint(w, oauthFailHTML(desc))
		stopOAuthServer()
		return
	}
	if code == "" {
		fmt.Fprint(w, oauthFailHTML("認可コードがありません"))
		return
	}

	fmt.Fprint(w, oauthSuccessHTML())
	go func() {
		exchangeAndUpdate(code)
		stopOAuthServer()
	}()
}

func waitForCallback() {
	select {
	case ok := <-oauthDone:
		if !ok {
			log.Printf("[OAuth] Failed: %s", oauthResult)
			return
		}
		log.Println("[OAuth] Authenticated!")
		saveEnvVars()
		if page == OA {
			show(TW, "", false)
		}
		go func() {
			if f := fetchFollowedFromAPI(); len(f) > 0 {
				followed = f
				saveFollowedToCache(f)
				fetchUsers(f)
			}
			fetchStreams()
			renderTW()
		}()
	case <-time.After(5 * time.Minute):
		log.Println("[OAuth] Timeout")
		stopOAuthServer()
	}
}

func stopOAuthServer() {
	if oauthServer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	oauthServer.Shutdown(ctx)
	oauthServer = nil
	log.Println("[OAuth] Server stopped")
}

func ensureScope(scope string) string {
	required := []string{"user:read:email", "user:read:follows", "user:read:broadcast"}
	parts := strings.Split(scope, " ")
	m := make(map[string]bool, len(parts))
	for _, p := range parts {
		m[p] = true
	}
	for _, r := range required {
		if !m[r] {
			parts = append(parts, r)
		}
	}
	return strings.Join(parts, " ")
}

func exchangeAndUpdate(code string) {
	cid, cs := clientID(), clientSecret()
	if cid == "" || cs == "" {
		log.Println("[OAuth] ERROR: Client ID/Secret not set")
		oauthDone <- false
		return
	}

	log.Println("[OAuth] Fetching token...")
	resp, err := http.PostForm("https://id.twitch.tv/oauth2/token", url.Values{
		"client_id":     {cid},
		"client_secret": {cs},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {"http://localhost:8080"},
	})
	if err != nil {
		log.Printf("[OAuth] Network error: %v", err)
		oauthDone <- false
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		log.Printf("[OAuth] Error %d: %s", resp.StatusCode, body)
		oauthResult = parseOAuthError(body)
		oauthDone <- false
		return
	}

	var res map[string]interface{}
	json.Unmarshal(body, &res)
	AT = fmt.Sprintf("%v", res["access_token"])
	RT = fmt.Sprintf("%v", res["refresh_token"])

	if u := fetchOAuthUser(cid, AT); u != nil {
		UID = u.ID
	}
	oauthDone <- true
}

type twitchUser struct {
	ID, Login, DisplayName string
}

func fetchOAuthUser(cid, token string) *twitchUser {
	req, _ := http.NewRequest("GET", "https://api.twitch.tv/helix/users", nil)
	req.Header.Set("Client-ID", cid)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()
	var r struct {
		Data []struct {
			ID, Login, DisplayName string
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	if len(r.Data) == 0 {
		return nil
	}
	return &twitchUser{r.Data[0].ID, r.Data[0].Login, r.Data[0].DisplayName}
}

func parseOAuthError(body []byte) string {
	s := string(body)
	if strings.Contains(s, "invalid client secret") {
		return "Client Secretが無効です。config.jsonを確認してください"
	}
	if strings.Contains(s, "invalid") {
		return "認証情報無効: " + s
	}
	return fmt.Sprintf("HTTP error: %s", s)
}

func saveEnvVars() {
	cid, cs := clientID(), clientSecret()
	if cid == "" || cs == "" {
		log.Println("[OAuth] Nothing to save (no credentials)")
		return
	}

	cfg := loadConfigFromFile()
	updated := false
	if cfg.ClientID != cid {
		cfg.ClientID = cid
		updated = true
	}
	if cfg.ClientSecret != cs {
		cfg.ClientSecret = cs
		updated = true
	}
	if cfg.Scope == "" {
		cfg.Scope = SCOPE
		updated = true
	}

	if updated && saveConfig(cfg) {
		log.Println("[OAuth] Config saved")
	}

	if AT != "" {
		if tokenManager == nil {
			tokenManager = NewTokenManager()
		}
		tokenManager.SaveToken(&TokenInfo{
			AccessToken:  AT,
			RefreshToken: RT,
			UserID:       UID,
			ClientID:     cid,
			Scope:        SCOPE,
			ExpiresAt:    time.Now().Add(24 * time.Hour),
			CreatedAt:    time.Now(),
			LastUsed:     time.Now(),
		})
	}
}

func getTokenFromClipboard() {
	code, err := platformGetClipboard()
	if err != nil {
		log.Printf("[OAuth] Clipboard error: %v", err)
		return
	}
	if strings.Contains(code, "code=") {
		if u, err := url.Parse(code); err == nil {
			if c := u.Query().Get("code"); c != "" {
				code = c
			}
		}
	}
	code = strings.TrimSpace(code)
	if code == "" {
		log.Println("[OAuth] No code in clipboard")
		return
	}
	log.Printf("[OAuth] Code from clipboard: %s...", code[:min(10, len(code))])
	exchangeAndUpdate(code)
}

// HTML templates
func oauthSuccessHTML() string {
	return `<!DOCTYPE html><html><head><meta charset="utf-8"><title>OK</title><style>body{font-family:sans-serif;text-align:center;padding:50px;background:#1a1a2e;color:#eee}h1{color:#4ecdc4}</style></head><body><h1>認証成功</h1><p>StreamDeckがトークンを取得中...</p><p style="color:#aaa">このウィンドウは閉じてください</p></body></html>`
}

func oauthFailHTML(desc string) string {
	return `<!DOCTYPE html><html><head><meta charset="utf-8"><title>Fail</title><style>body{font-family:sans-serif;text-align:center;padding:50px;background:#1a1a2e;color:#eee}h1{color:#ff6b6b}</style></head><body><h1>認証失敗</h1><p>` + desc + `</p><p style="color:#aaa">このウィンドウは閉じてください</p></body></html>`
}