package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// リモート環境変数ストア設定 (S8+ Termux)
var (
	remoteEnvHost    = "192.168.0.98"
	remoteEnvPort    = "9090"
	remoteEnvTimeout = 3 * time.Second
)

// getRemoteEnvURL returns the current remote env store URL
func getRemoteEnvURL() string {
	return fmt.Sprintf("http://%s:%s", remoteEnvHost, remoteEnvPort)
}

// fetchRemoteEnv fetches all env vars from remote HTTP server
func fetchRemoteEnv() map[string]string {
	client := &http.Client{Timeout: remoteEnvTimeout}
	resp, err := client.Get(getRemoteEnvURL())
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	result := make(map[string]string)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, `"'`)
			if key != "" {
				result[key] = value
			}
		}
	}
	return result
}

// getEnvOrRemote gets an env var locally first, then falls back to remote store
func getEnvOrRemote(name string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return ""
}

// loadEnvFromRemote loads all Twitch env vars from remote store
// Only fills in vars that are empty locally
func loadEnvFromRemote() {
	envMap := fetchRemoteEnv()
	if envMap == nil {
		log.Println("[ENV] リモート環境変数ストアに接続できません。ローカル環境変数のみ使用します。")
		return
	}

	loaded := 0
	if CID == "" {
		if v, ok := envMap["TWITCH_CLIENT_ID"]; ok && v != "" {
			CID = v
			loaded++
		}
	}
	if CS == "" {
		if v, ok := envMap["TWITCH_CLIENT_SECRET"]; ok && v != "" {
			CS = v
			loaded++
		}
	}
	if AT == "" {
		if v, ok := envMap["TWITCH_ACCESS_TOKEN"]; ok && v != "" {
			AT = v
			loaded++
		}
	}
	if RT == "" {
		if v, ok := envMap["TWITCH_REFRESH_TOKEN"]; ok && v != "" {
			RT = v
			loaded++
		}
	}
	if UID == "" {
		if v, ok := envMap["TWITCH_USER_ID"]; ok && v != "" {
			UID = v
			loaded++
		}
	}
	if IRC_T == "" {
		if v, ok := envMap["TWITCH_IRC_TOKEN"]; ok && v != "" {
			IRC_T = v
			loaded++
		}
	}

	if loaded > 0 {
		log.Printf("[ENV] リモートストア (%s) から %d 個の環境変数を読み込みました\n", getRemoteEnvURL(), loaded)
	} else {
		log.Printf("[ENV] リモートストア (%s) に接続成功。追加で読み込む環境変数はありませんでした\n", getRemoteEnvURL())
	}
}
