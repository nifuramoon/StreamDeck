//go:build linux

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
