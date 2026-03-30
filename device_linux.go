//go:build linux

package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func openStreamDeck() (*V2Device, error) {
	for attempt := 0; attempt < 10; attempt++ {
		files, err := os.ReadDir("/sys/class/hidraw")
		if err == nil {
			for _, f := range files {
				uevent, err := os.ReadFile(filepath.Join("/sys/class/hidraw", f.Name(), "device", "uevent"))
				if err != nil {
					continue
				}
				ueventStr := strings.ToUpper(string(uevent))
				if strings.Contains(ueventStr, "0FD9") && strings.Contains(ueventStr, "006D") {
					devPath := "/dev/" + f.Name()
					file, err := os.OpenFile(devPath, os.O_RDWR, 0)
					if err == nil {
						log.Println("[USB] Stream Deck Connected natively via", devPath)
						return &V2Device{
							file:       file,
							prevImages: make([]string, MAX_KEYS),
						}, nil
					}
				}
			}
		}
		exec.Command("sudo", "usbreset", "0fd9:006d").Run()
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("device not found or permission denied")
}

func platformSetBrightness(fd uintptr, payload []byte) error {
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, fd, 0xC0204806, uintptr(unsafe.Pointer(&payload[0])))
	if err != 0 {
		return fmt.Errorf("ioctl error: %v", err)
	}
	return nil
}

func platformOpenBrowser(url string) {
	exec.Command("xdg-open", url).Start()
}

func platformReboot() {
	log.Println("[SYSTEM] システム再起動を実行します...")

	// 複数の方法で再起動を試みる
	commands := [][]string{
		{"systemctl", "reboot"},
		{"shutdown", "-r", "now"},
		{"reboot"},
	}

	for _, cmdArgs := range commands {
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		if err := cmd.Start(); err == nil {
			log.Printf("[SYSTEM] 再起動コマンド実行: %v", cmdArgs)
			// コマンドが成功したら終了
			return
		} else {
			log.Printf("[SYSTEM] 再起動コマンド失敗: %v - %v", cmdArgs, err)
		}
	}

	log.Println("[SYSTEM] 警告: 再起動コマンドが実行できませんでした")
}

func platformLoadFontPaths() []string {
	return []string{
		// Japanese fonts FIRST (priority for Japanese text)
		"/usr/share/fonts/OTF/ipag.ttf",  // IPA Gothic (installed)
		"/usr/share/fonts/OTF/ipagp.ttf", // IPA Gothic P
		"/usr/share/fonts/OTF/ipam.ttf",  // IPA Mincho
		"/usr/share/fonts/OTF/ipamp.ttf", // IPA Mincho P

		// Noto CJK fonts (excellent Japanese support)
		"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/noto-cjk/NotoSansCJK-Bold.ttc",
		"/usr/share/fonts/noto-cjk/NotoSansCJP-Regular.ttc", // Japanese-specific
		"/usr/share/fonts/noto-cjk/NotoSansCJP-Bold.ttc",    // Japanese-specific

		// Other Japanese fonts
		"/usr/share/fonts/truetype/fonts-japanese-gothic.ttf",
		"/usr/share/fonts/truetype/fonts-japanese-mincho.ttf",
		"/usr/share/fonts/TTF/ume-ui-gothic.ttf",    // UME UI Gothic
		"/usr/share/fonts/TTF/ume-gothic.ttf",       // UME Gothic
		"/usr/share/fonts/TTF/ume-mincho.ttf",       // UME Mincho
		"/usr/share/fonts/truetype/mona/mona.ttf",   // Mona font
		"/usr/share/fonts/truetype/mona/monab.ttf",  // Mona bold
		"/usr/share/fonts/truetype/mona/monapo.ttf", // Mona proportional

		// Alternative locations for Japanese fonts
		"/usr/share/fonts/opentype/ipafont/ipag.ttf",
		"/usr/share/fonts/truetype/ipafont/ipag.ttf",
		"/usr/share/fonts/opentype/ipafont/ipam.ttf",
		"/usr/share/fonts/truetype/ipafont/ipam.ttf",

		// Other CJK fonts
		"/usr/share/fonts/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/noto/NotoSansCJK-Bold.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Bold.ttc",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Bold.ttc",
		"/usr/share/fonts/google-noto-cjk/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/google-noto-cjk/NotoSansCJK-Bold.ttc",

		// English/Latin fonts as fallback
		"/usr/share/fonts/liberation/LiberationSans-Regular.ttf",
		"/usr/share/fonts/liberation/LiberationSans-Bold.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/usr/share/fonts/TTF/DejaVuSans-Bold.ttf",
		"/usr/share/fonts/TTF/FreeSans.ttf",
		"/usr/share/fonts/Adwaita/AdwaitaSans-Regular.ttf",
		"/usr/share/fonts/Adwaita/AdwaitaSans-Bold.ttf",
	}
}

// platformGetClipboard gets text from clipboard on Linux
func platformGetClipboard() (string, error) {
	// xclipを使用してクリップボードを取得
	cmd := exec.Command("xclip", "-selection", "clipboard", "-o")
	output, err := cmd.Output()
	if err != nil {
		// xclipが失敗した場合、xselを試す
		cmd = exec.Command("xsel", "--clipboard", "--output")
		output, err = cmd.Output()
		if err != nil {
			return "", fmt.Errorf("クリップボード取得に失敗: %v", err)
		}
	}
	return strings.TrimSpace(string(output)), nil
}

// platformSetEnvVar sets environment variable on Linux
func platformSetEnvVar(name, value string) bool {
	// 現在のプロセスの環境変数を設定
	os.Setenv(name, value)

	// ~/.bashrcにも保存（永続化）
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	bashrc := home + "/.bashrc"
	content, err := os.ReadFile(bashrc)
	if err != nil {
		// ファイルが存在しない場合は作成
		content = []byte{}
	}

	// 既存の設定を置換
	lines := strings.Split(string(content), "\n")
	newLines := []string{}
	found := false
	exportLine := fmt.Sprintf(`export %s="%s"`, name, value)

	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "export "+name+"=") {
			newLines = append(newLines, exportLine)
			found = true
		} else {
			newLines = append(newLines, line)
		}
	}

	if !found {
		newLines = append(newLines, exportLine)
	}

	err = os.WriteFile(bashrc, []byte(strings.Join(newLines, "\n")), 0644)
	return err == nil
}

// platformSpeakText implements text-to-speech on Linux
func platformSpeakText(text string) {
	// VoiceVox Engineを最優先で試す（最も自然な音声）
	if voicevoxSpeak(text) {
		return
	}

	// Try multiple TTS engines in order of preference
	// より自然な音声のエンジンを優先
	engines := []struct {
		name string
		args []string
		desc string
	}{
		// 1. espeak-ng with optimized parameters for Japanese (most available)
		{"espeak-ng", []string{"-v", "ja", "-s", "130", "-p", "40", "-a", "200", text}, "espeak-ng (最適化)"},
		// 2. espeak (legacy)
		{"espeak", []string{"-v", "ja", "-s", "130", text}, "espeak"},
		// 3. spd-say with Japanese voice if available
		{"spd-say", []string{"-r", "40", "-p", "30", "-y", "japanese", text}, "speech-dispatcher (日本語)"},
		// 4. spd-say fallback
		{"spd-say", []string{"-r", "40", "-p", "30", text}, "speech-dispatcher"},
		// 5. festival with Japanese if configured
		{"festival", []string{"--tts"}, "festival"},
	}

	// まず利用可能なエンジンを確認
	availableEngines := []struct {
		name string
		args []string
		desc string
	}{}

	for _, engine := range engines {
		if _, err := exec.LookPath(engine.name); err == nil {
			availableEngines = append(availableEngines, engine)
			log.Printf("[TTS] 利用可能なエンジン: %s", engine.desc)
		}
	}

	if len(availableEngines) == 0 {
		log.Printf("[TTS] 警告: 利用可能な音声合成エンジンが見つかりません")
		log.Printf("[TTS] インストール可能なパッケージ (Arch Linux):")
		log.Printf("[TTS]   - espeak-ng: sudo pacman -S espeak-ng")
		log.Printf("[TTS]   - speech-dispatcher: sudo pacman -S speech-dispatcher")
		log.Printf("[TTS]   - festival: sudo pacman -S festival")
		log.Printf("[TTS]   - VoiceVox Engine: https://voicevox.hiroshiba.jp/")
		return
	}

	// 利用可能なエンジンで試行
	for _, engine := range availableEngines {
		cmd := exec.Command(engine.name, engine.args...)

		// Handle stdin for festival
		if engine.name == "festival" {
			cmd.Stdin = strings.NewReader(text)
		}

		// 直接再生するエンジン
		if err := cmd.Start(); err == nil {
			log.Printf("[TTS] 音声合成成功: %s (%s)", text, engine.desc)

			// 非同期で終了を待機（ゾンビプロセス防止）
			go func() {
				cmd.Wait()
			}()

			return
		} else {
			log.Printf("[TTS] エンジン %s 失敗: %v", engine.desc, err)
		}
	}

	log.Printf("[TTS] 警告: すべての音声合成エンジンが失敗しました")
}

// voicevoxSpeak uses VoiceVox Engine for high-quality Japanese TTS
func voicevoxSpeak(text string) bool {
	// VoiceVox Engineのデフォルトポート
	voicevoxURL := "http://127.0.0.1:50021"

	// 1. VoiceVox Engineが起動しているか確認
	resp, err := http.Get(voicevoxURL + "/version")
	if err != nil {
		// VoiceVox Engineが起動していない
		log.Printf("[TTS] VoiceVox Engine 接続失敗: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("[TTS] VoiceVox Engine APIエラー: %d", resp.StatusCode)
		return false
	}

	log.Printf("[TTS] ✅ VoiceVox Engine を検出しました")

	// 2. 音声クエリを生成（デフォルトスピーカー: 四国めたん）
	speakerID := 2 // 四国めたん（ノーマル）

	// 音声クエリパラメータ
	queryParams := url.Values{}
	queryParams.Set("text", text)
	queryParams.Set("speaker", strconv.Itoa(speakerID))

	// 音声クエリを取得
	queryURL := voicevoxURL + "/audio_query?" + queryParams.Encode()
	queryReq, err := http.NewRequest("POST", queryURL, nil)
	if err != nil {
		log.Printf("[TTS] VoiceVox クエリ作成失敗: %v", err)
		return false
	}

	queryResp, err := httpClient.Do(queryReq)
	if err != nil {
		log.Printf("[TTS] VoiceVox クエリ失敗: %v", err)
		return false
	}
	defer queryResp.Body.Close()

	if queryResp.StatusCode != 200 {
		log.Printf("[TTS] VoiceVox クエリエラー: %d", queryResp.StatusCode)
		return false
	}

	// クエリデータを読み込み
	queryData, err := io.ReadAll(queryResp.Body)
	if err != nil {
		log.Printf("[TTS] VoiceVox クエリデータ読み込み失敗: %v", err)
		return false
	}

	// 3. 音声合成を実行
	synthesisURL := voicevoxURL + "/synthesis?speaker=" + strconv.Itoa(speakerID)
	synthesisReq, err := http.NewRequest("POST", synthesisURL, bytes.NewReader(queryData))
	if err != nil {
		log.Printf("[TTS] VoiceVox 合成リクエスト作成失敗: %v", err)
		return false
	}
	synthesisReq.Header.Set("Content-Type", "application/json")

	synthesisResp, err := httpClient.Do(synthesisReq)
	if err != nil {
		log.Printf("[TTS] VoiceVox 合成失敗: %v", err)
		return false
	}
	defer synthesisResp.Body.Close()

	if synthesisResp.StatusCode != 200 {
		log.Printf("[TTS] VoiceVox 合成エラー: %d", synthesisResp.StatusCode)
		return false
	}

	// 4. 音声データを一時ファイルに保存
	wavData, err := io.ReadAll(synthesisResp.Body)
	if err != nil {
		log.Printf("[TTS] VoiceVox 音声データ読み込み失敗: %v", err)
		return false
	}

	wavFile := "/tmp/tts_voicevox.wav"
	if err := os.WriteFile(wavFile, wavData, 0644); err != nil {
		log.Printf("[TTS] VoiceVox 音声ファイル保存失敗: %v", err)
		return false
	}

	// 5. 音声を再生（PipeWire/PulseAudio）
	var playCmd *exec.Cmd
	if _, err := exec.LookPath("pw-play"); err == nil {
		playCmd = exec.Command("pw-play", wavFile) // PipeWire
	} else if _, err := exec.LookPath("paplay"); err == nil {
		playCmd = exec.Command("paplay", wavFile) // PulseAudio
	} else {
		playCmd = exec.Command("aplay", wavFile) // ALSA (fallback)
	}

	if err := playCmd.Start(); err != nil {
		log.Printf("[TTS] VoiceVox 音声再生失敗: %v", err)
		os.Remove(wavFile)
		return false
	}

	// 非同期で再生終了を待機し、一時ファイルを削除
	go func() {
		playCmd.Wait()
		time.Sleep(1 * time.Second) // 再生が確実に終わるのを待つ
		os.Remove(wavFile)
	}()

	log.Printf("[TTS] 音声合成成功: %s (VoiceVox Engine)", text)
	return true
}
