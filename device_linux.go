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
						return &V2Device{file: file}, nil
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
	exec.Command("systemctl", "reboot").Start()
}

func platformLoadFontPaths() []string {
	return []string{
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Bold.ttc",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Bold.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/ipafont/ipag.ttf",
		"/usr/share/fonts/truetype/fonts-japanese-gothic.ttf",
		"/usr/share/fonts/noto-cjk/NotoSansCJK-Bold.ttc",
		"/usr/share/fonts/google-noto-cjk/NotoSansCJK-Bold.ttc",
	}
}
