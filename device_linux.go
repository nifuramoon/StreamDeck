//go:build linux

package main

import (
	"bytes"
	"fmt"
	"image"
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
		if err != nil {
			goto retry
		}
		for _, f := range files {
			uevent, err := os.ReadFile(filepath.Join("/sys/class/hidraw", f.Name(), "device", "uevent"))
			if err != nil {
				continue
			}
			if !(strings.Contains(strings.ToUpper(string(uevent)), "0FD9") &&
				strings.Contains(strings.ToUpper(string(uevent)), "006D")) {
				continue
			}
			devPath := "/dev/" + f.Name()
			file, err := os.OpenFile(devPath, os.O_RDWR, 0)
			if err != nil {
				continue
			}
			log.Println("[USB] Stream Deck Connected natively via", devPath)
			return &V2Device{
				file:       file,
				prevImages: make([]string, MAX_KEYS),
			}, nil
		}
	retry:
		exec.Command("sudo", "usbreset", "0fd9:006d").Run()
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("device not found or permission denied")
}

func platformSetBrightness(fd uintptr, payload []byte) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, 0xC0204806, uintptr(unsafe.Pointer(&payload[0])))
	if errno != 0 {
		return fmt.Errorf("ioctl error: %v", errno)
	}
	return nil
}

func platformOpenBrowser(url string) {
	exec.Command("xdg-open", url).Start()
}

func platformReboot() {
	log.Println("[SYSTEM] システム再起動を実行します...")
	for _, args := range [][]string{
		{"systemctl", "reboot"},
		{"shutdown", "-r", "now"},
		{"reboot"},
	} {
		if err := exec.Command(args[0], args[1:]...).Start(); err == nil {
			log.Printf("[SYSTEM] 再起動コマンド実行: %v", args)
			return
		} else {
			log.Printf("[SYSTEM] 再起動コマンド失敗: %v - %v", args, err)
		}
	}
	log.Println("[SYSTEM] 警告: 再起動コマンドが実行できませんでした")
}

var g_forcedFontPath string

func platformSetFontPath(path string) {
	g_forcedFontPath = path
}

func platformLoadFontPaths() []string {
	common := []string{
		"/usr/share/fonts/noto-cjk/NotoSansCJK-Bold.ttc",
		"/usr/share/fonts/liberation/LiberationSerif-Regular.ttf",
		"/usr/share/fonts/liberation/LiberationSans-Regular.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/usr/share/fonts/Adwaita/AdwaitaSans-Regular.ttf",
	}
	if g_forcedFontPath != "" {
		return append([]string{g_forcedFontPath, "/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc"}, common...)
	}
	return common
}

func platformGetClipboard() (string, error) {
	for _, cmd := range []*exec.Cmd{
		exec.Command("xclip", "-selection", "clipboard", "-o"),
		exec.Command("xsel", "--clipboard", "--output"),
	} {
		if out, err := cmd.Output(); err == nil {
			return strings.TrimSpace(string(out)), nil
		}
	}
	return "", fmt.Errorf("クリップボード取得に失敗: xclip/xselが利用できません")
}

func platformSetEnvVar(name, value string) bool {
	os.Setenv(name, value)

	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	bashrc := filepath.Join(home, ".bashrc")
	content, _ := os.ReadFile(bashrc)

	var lines []string
	found := false
	prefix := "export " + name + "="
	exportLine := fmt.Sprintf(`export %s="%s"`, name, value)

	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			lines = append(lines, exportLine)
			found = true
		} else {
			lines = append(lines, line)
		}
	}
	if !found {
		lines = append(lines, exportLine)
	}

	return os.WriteFile(bashrc, []byte(strings.Join(lines, "\n")+"\n"), 0644) == nil
}

func platformSpeakText(text string) {
	if voicevoxSpeak(text) {
		return
	}

	engines := []struct {
		name string
		args []string
		fn   func() *exec.Cmd
	}{
		{"espeak-ng", nil, func() *exec.Cmd { return exec.Command("espeak-ng", "-v", "ja", "-s", "130", "-p", "40", "-a", "200", text) }},
		{"espeak", nil, func() *exec.Cmd { return exec.Command("espeak", "-v", "ja", "-s", "130", text) }},
		{"spd-say", nil, func() *exec.Cmd { return exec.Command("spd-say", "-r", "40", "-p", "30", "-y", "japanese", text) }},
		{"spd-say", nil, func() *exec.Cmd { return exec.Command("spd-say", "-r", "40", "-p", "30", text) }},
		{"festival", nil, func() *exec.Cmd {
			cmd := exec.Command("festival", "--tts")
			cmd.Stdin = strings.NewReader(text)
			return cmd
		}},
	}

	var available []struct {
		name string
		fn   func() *exec.Cmd
	}
	for _, e := range engines {
		if _, err := exec.LookPath(e.name); err == nil {
			available = append(available, struct {
				name string
				fn   func() *exec.Cmd
			}{e.name, e.fn})
			log.Printf("[TTS] 利用可能なエンジン: %s", e.name)
		}
	}

	if len(available) == 0 {
		log.Println("[TTS] 警告: 利用可能な音声合成エンジンが見つかりません")
		log.Println("[TTS] インストール可能なパッケージ (Arch Linux):")
		log.Println("[TTS]   - espeak-ng: sudo pacman -S espeak-ng")
		log.Println("[TTS]   - speech-dispatcher: sudo pacman -S speech-dispatcher")
		log.Println("[TTS]   - festival: sudo pacman -S festival")
		log.Println("[TTS]   - VoiceVox Engine: https://voicevox.hiroshiba.jp/")
		return
	}

	for _, e := range available {
		cmd := e.fn()
		if err := cmd.Start(); err == nil {
			log.Printf("[TTS] 音声合成成功: %s (%s)", text, e.name)
			go func() { cmd.Wait() }()
			return
		}
		log.Printf("[TTS] エンジン %s 失敗: %v", e.name, err)
	}

	log.Println("[TTS] 警告: すべての音声合成エンジンが失敗しました")
}

func voicevoxSpeak(text string) bool {
	const baseURL = "http://127.0.0.1:50021"
	const speakerID = 2 // 四国めたん（ノーマル）

	if !voicevoxPing(baseURL) {
		return false
	}

	queryData, err := voicevoxPost(baseURL+"/audio_query?"+url.Values{
		"text":    {text},
		"speaker": {strconv.Itoa(speakerID)},
	}.Encode(), nil)
	if err != nil {
		log.Printf("[TTS] VoiceVox クエリ失敗: %v", err)
		return false
	}

	wavData, err := voicevoxPost(baseURL+"/synthesis?speaker="+strconv.Itoa(speakerID), bytes.NewReader(queryData))
	if err != nil {
		log.Printf("[TTS] VoiceVox 合成失敗: %v", err)
		return false
	}

	wavFile := "/tmp/tts_voicevox.wav"
	if err := os.WriteFile(wavFile, wavData, 0644); err != nil {
		log.Printf("[TTS] VoiceVox 音声ファイル保存失敗: %v", err)
		return false
	}
	defer os.Remove(wavFile)

	playCmd := pickPlayer(wavFile)
	if err := playCmd.Start(); err != nil {
		log.Printf("[TTS] VoiceVox 音声再生失敗: %v", err)
		return false
	}

	go func() {
		playCmd.Wait()
		time.Sleep(time.Second)
	}()

	log.Printf("[TTS] 音声合成成功: %s (VoiceVox Engine)", text)
	return true
}

func voicevoxPing(baseURL string) bool {
	resp, err := http.Get(baseURL + "/version")
	if err != nil {
		log.Printf("[TTS] VoiceVox Engine 接続失敗: %v", err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[TTS] VoiceVox Engine APIエラー: %d", resp.StatusCode)
		return false
	}
	log.Println("[TTS] ✅ VoiceVox Engine を検出しました")
	return true
}

func voicevoxPost(url string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func pickPlayer(file string) *exec.Cmd {
	for _, bin := range []string{"pw-play", "paplay", "aplay"} {
		if _, err := exec.LookPath(bin); err == nil {
			return exec.Command(bin, file)
		}
	}
	return exec.Command("aplay", file) // fallback
}

func flipV2(img image.Image) *image.RGBA {
	b := img.Bounds()
	res := image.NewRGBA(b)
	w, h := b.Dx(), b.Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			res.Set(w-1-x, h-1-y, img.At(x, y))
		}
	}
	return res
}