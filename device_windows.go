//go:build windows

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

var (
	setupapi                          = syscall.NewLazyDLL("setupapi.dll")
	hid                               = syscall.NewLazyDLL("hid.dll")
	kernel32                          = syscall.NewLazyDLL("kernel32.dll")
	pSetupDiGetClassDevs              = setupapi.NewProc("SetupDiGetClassDevsW")
	pSetupDiEnumDeviceInterfaces      = setupapi.NewProc("SetupDiEnumDeviceInterfaces")
	pSetupDiGetDeviceInterfaceDetailW = setupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
	pHidD_GetAttributes               = hid.NewProc("HidD_GetAttributes")
	pCreateFileW                      = kernel32.NewProc("CreateFileW")
)

var hidGUID = syscall.GUID{
	Data1: 0x4D1E55B2,
	Data2: 0xF16F,
	Data3: 0x11CF,
	Data4: [8]byte{0x88, 0xCB, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30},
}

type hidAttributes struct {
	Size, VendorID, ProductID, VersionNumber uint16
}

type spDeviceInterfaceData struct {
	cbSize             uint32
	classGuid          syscall.GUID
	flags              uint32
	reserved           uintptr
}

func openStreamDeck() (*V2Device, error) {
	for i := 0; i < 10; i++ {
		if dev, err := findHIDDevice(0x0FD9, 0x006D); err == nil {
			return dev, nil
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("Stream Deck device not found")
}

func findHIDDevice(vid, pid uint16) (*V2Device, error) {
	hDevInfo, _, _ := pSetupDiGetClassDevs.Call(
		uintptr(unsafe.Pointer(&hidGUID)), 0, 0, 0x12,
	)
	if hDevInfo == 0 || hDevInfo == ^uintptr(0) {
		return nil, fmt.Errorf("SetupDiGetClassDevs failed")
	}

	var did spDeviceInterfaceData
	did.cbSize = uint32(unsafe.Sizeof(did))

	for i := uint32(0); ; i++ {
		ret, _, _ := pSetupDiEnumDeviceInterfaces.Call(
			hDevInfo, 0, uintptr(unsafe.Pointer(&hidGUID)), uintptr(i),
			uintptr(unsafe.Pointer(&did)),
		)
		if ret == 0 {
			break
		}

		var reqSize uint32
		pSetupDiGetDeviceInterfaceDetailW.Call(
			hDevInfo, uintptr(unsafe.Pointer(&did)), 0, 0,
			uintptr(unsafe.Pointer(&reqSize)), 0,
		)

		buf := make([]byte, reqSize)
		*(*uint32)(unsafe.Pointer(&buf[0])) = uint32(4 + unsafe.Sizeof(uintptr(0)))

		ret, _, _ = pSetupDiGetDeviceInterfaceDetailW.Call(
			hDevInfo, uintptr(unsafe.Pointer(&did)),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(reqSize), 0, 0,
		)
		if ret == 0 {
			continue
		}

		path := syscall.UTF16ToString((*[1024]uint16)(unsafe.Pointer(&buf[4]))[:])
		up := strings.ToUpper(path)
		if !strings.Contains(up, fmt.Sprintf("VID_%04X", vid)) ||
			!strings.Contains(up, fmt.Sprintf("PID_%04X", pid)) {
			continue
		}

		ptr, _ := syscall.UTF16PtrFromString(path)
		h, _, _ := pCreateFileW.Call(
			uintptr(unsafe.Pointer(ptr)),
			syscall.GENERIC_READ|syscall.GENERIC_WRITE,
			syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
			0, syscall.OPEN_EXISTING, 0, 0,
		)
		if h == ^uintptr(0) {
			continue
		}

		log.Printf("[USB] Stream Deck Connected via %s", path)
		return &V2Device{
			file:       os.NewFile(h, path),
			prevImages: make([]string, MAX_KEYS),
		}, nil
	}
	return nil, fmt.Errorf("device VID=%04X PID=%04X not found", vid, pid)
}

func platformSetBrightness(fd uintptr, payload []byte) error {
	var written uint32
	err := syscall.DeviceIoControl(
		syscall.Handle(fd), 0x000B0191,
		&payload[0], uint32(len(payload)), nil, 0, &written, nil,
	)
	if err != nil {
		_, err = os.NewFile(fd, "streamdeck").Write(payload)
	}
	return err
}

func platformOpenBrowser(url string) {
	exec.Command("cmd", "/c", "start", url).Start()
}

func platformReboot() {
	log.Println("[SYSTEM] システム再起動を実行します...")
	cmd := exec.Command("shutdown", "/r", "/t", "5", "/c", "StreamDeckから再起動を実行しました")
	if err := cmd.Start(); err != nil {
		log.Printf("[SYSTEM] 再起動コマンド失敗: %v", err)
		exec.Command("shutdown", "/r").Start()
		return
	}
	log.Println("[SYSTEM] 再起動コマンド実行: 5秒後に再起動します")
}

func platformLoadFontPaths() []string {
	winDir := os.Getenv("WINDIR")
	if winDir == "" {
		winDir = `C:\Windows`
	}
	fontsDir := filepath.Join(winDir, "Fonts")

	paths := []string{
		filepath.Join(fontsDir, "msgothic.ttc"),
		filepath.Join(fontsDir, "msmincho.ttc"),
		filepath.Join(fontsDir, "meiryo.ttc"),
		filepath.Join(fontsDir, "meiryob.ttc"),
		filepath.Join(fontsDir, "YuGothB.ttc"),
		filepath.Join(fontsDir, "YuGothM.ttc"),
		filepath.Join(fontsDir, "YuGothL.ttc"),
		filepath.Join(fontsDir, "yumin.ttf"),
		filepath.Join(fontsDir, "yumindb.ttf"),
		filepath.Join(fontsDir, "ipag.ttf"),
		filepath.Join(fontsDir, "ipam.ttf"),
		filepath.Join(fontsDir, "segoeui.ttf"),
		filepath.Join(fontsDir, "segoeuib.ttf"),
		filepath.Join(fontsDir, "arial.ttf"),
		filepath.Join(fontsDir, "arialbd.ttf"),
		filepath.Join(fontsDir, "tahoma.ttf"),
		filepath.Join(fontsDir, "tahomabd.ttf"),
	}

	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		userFonts := filepath.Join(localAppData, "Microsoft", "Windows", "Fonts")
		for _, f := range []string{"NotoSansCJKjp-Bold.otf", "NotoSansCJKjp-Regular.otf"} {
			paths = append(paths, filepath.Join(userFonts, f))
		}
	}

	return paths
}

func platformGetClipboard() (string, error) {
	out, err := exec.Command("powershell", "-Command", "Get-Clipboard").Output()
	if err != nil {
		return "", fmt.Errorf("clipboard error: %v", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func platformSetEnvVar(name, value string) bool {
	os.Setenv(name, value)
	return exec.Command("setx", name, value).Run() == nil
}

func platformSpeakText(text string) {
	// PowerShell SpeechSynthesizer (Windows 8+)
	script := fmt.Sprintf(`Add-Type -AssemblyName System.speech; (New-Object System.Speech.Synthesis.SpeechSynthesizer).Speak("%s")`,
		strings.ReplaceAll(text, `"`, `\"`))
	if err := exec.Command("powershell", "-Command", script).Start(); err == nil {
		log.Printf("[TTS] Success: %s", text)
		return
	}
	log.Printf("[TTS] Failed: %s", text)
}