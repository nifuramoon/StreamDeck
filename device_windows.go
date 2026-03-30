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

// HID GUID
var hidGUID = syscall.GUID{
	Data1: 0x4D1E55B2,
	Data2: 0xF16F,
	Data3: 0x11CF,
	Data4: [8]byte{0x88, 0xCB, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30},
}

type hidAttributes struct {
	Size          uint32
	VendorID      uint16
	ProductID     uint16
	VersionNumber uint16
}

type spDeviceInterfaceData struct {
	cbSize    uint32
	classGuid syscall.GUID
	flags     uint32
	reserved  uintptr
}

func openStreamDeck() (*V2Device, error) {
	for attempt := 0; attempt < 10; attempt++ {
		dev, err := findHIDDevice(0x0FD9, 0x006D)
		if err == nil {
			return dev, nil
		}
		log.Printf("[USB] Attempt %d: %v, retrying...", attempt+1, err)
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("Stream Deck device not found")
}

func findHIDDevice(vendorID, productID uint16) (*V2Device, error) {
	// Windows HID enumeration
	hDevInfo, _, _ := pSetupDiGetClassDevs.Call(
		uintptr(unsafe.Pointer(&hidGUID)),
		0, 0, 0x00000010|0x00000002,
	)
	if hDevInfo == 0 || hDevInfo == ^uintptr(0) {
		return nil, fmt.Errorf("SetupDiGetClassDevs failed")
	}

	var devInterfaceData spDeviceInterfaceData
	devInterfaceData.cbSize = uint32(unsafe.Sizeof(devInterfaceData))

	for i := uint32(0); ; i++ {
		ret, _, _ := pSetupDiEnumDeviceInterfaces.Call(
			hDevInfo, 0,
			uintptr(unsafe.Pointer(&hidGUID)),
			uintptr(i),
			uintptr(unsafe.Pointer(&devInterfaceData)),
		)
		if ret == 0 {
			break
		}

		// Get detail size
		var requiredSize uint32
		pSetupDiGetDeviceInterfaceDetailW.Call(
			hDevInfo,
			uintptr(unsafe.Pointer(&devInterfaceData)),
			0, 0,
			uintptr(unsafe.Pointer(&requiredSize)),
			0,
		)

		detailBuf := make([]byte, requiredSize)
		// cbSize for SP_DEVICE_INTERFACE_DETAIL_DATA_W on 64-bit is 8, on 32-bit is 6
		if unsafe.Sizeof(uintptr(0)) == 8 {
			*(*uint32)(unsafe.Pointer(&detailBuf[0])) = 8
		} else {
			*(*uint32)(unsafe.Pointer(&detailBuf[0])) = 6
		}

		ret, _, _ = pSetupDiGetDeviceInterfaceDetailW.Call(
			hDevInfo,
			uintptr(unsafe.Pointer(&devInterfaceData)),
			uintptr(unsafe.Pointer(&detailBuf[0])),
			uintptr(requiredSize),
			0, 0,
		)
		if ret == 0 {
			continue
		}

		// Extract device path (starts at offset 4)
		devicePath := syscall.UTF16ToString((*[1024]uint16)(unsafe.Pointer(&detailBuf[4]))[:])

		// Check VID/PID in path string
		pathUpper := strings.ToUpper(devicePath)
		vidStr := fmt.Sprintf("VID_%04X", vendorID)
		pidStr := fmt.Sprintf("PID_%04X", productID)
		if !strings.Contains(pathUpper, vidStr) || !strings.Contains(pathUpper, pidStr) {
			continue
		}

		// Open device
		pathPtr, _ := syscall.UTF16PtrFromString(devicePath)
		handle, _, _ := pCreateFileW.Call(
			uintptr(unsafe.Pointer(pathPtr)),
			syscall.GENERIC_READ|syscall.GENERIC_WRITE,
			syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
			0,
			syscall.OPEN_EXISTING,
			0, 0,
		)
		if handle == ^uintptr(0) {
			continue
		}

		file := os.NewFile(handle, devicePath)
		log.Printf("[USB] Stream Deck Connected via %s", devicePath)
		return &V2Device{
			file:       file,
			prevImages: make([]string, MAX_KEYS),
		}, nil
	}

	return nil, fmt.Errorf("device VID=%04X PID=%04X not found", vendorID, productID)
}

func platformSetBrightness(fd uintptr, payload []byte) error {
	// Windows: HID Feature Report
	var written uint32
	err := syscall.DeviceIoControl(
		syscall.Handle(fd),
		0x000B0191, // IOCTL_HID_SET_FEATURE
		&payload[0], uint32(len(payload)),
		nil, 0, &written, nil,
	)
	if err != nil {
		// Fallback: direct write
		f := os.NewFile(fd, "streamdeck")
		_, writeErr := f.Write(payload)
		if writeErr != nil {
			return fmt.Errorf("brightness set failed: %v", writeErr)
		}
	}
	return nil
}

func platformOpenBrowser(url string) {
	exec.Command("cmd", "/c", "start", url).Start()
}

func platformReboot() {
	log.Println("[SYSTEM] システム再起動を実行します...")

	// Windowsでの再起動（管理者権限が必要な場合がある）
	cmd := exec.Command("shutdown", "/r", "/t", "5", "/c", "StreamDeckから再起動を実行しました")
	if err := cmd.Start(); err != nil {
		log.Printf("[SYSTEM] 再起動コマンド失敗: %v", err)
		// 代替方法
		exec.Command("shutdown", "/r").Start()
	} else {
		log.Println("[SYSTEM] 再起動コマンド実行: 5秒後に再起動します")
	}
}

func platformLoadFontPaths() []string {
	winDir := os.Getenv("WINDIR")
	if winDir == "" {
		winDir = `C:\Windows`
	}
	fontsDir := filepath.Join(winDir, "Fonts")
	localAppData := os.Getenv("LOCALAPPDATA")

	paths := []string{
		// 日本語フォントを最優先
		filepath.Join(fontsDir, "msgothic.ttc"), // MS ゴシック
		filepath.Join(fontsDir, "msmincho.ttc"), // MS 明朝
		filepath.Join(fontsDir, "meiryo.ttc"),   // メイリオ
		filepath.Join(fontsDir, "meiryob.ttc"),  // メイリオ 太字
		filepath.Join(fontsDir, "YuGothB.ttc"),  // 游ゴシック Bold
		filepath.Join(fontsDir, "YuGothM.ttc"),  // 游ゴシック Medium
		filepath.Join(fontsDir, "YuGothL.ttc"),  // 游ゴシック Light
		filepath.Join(fontsDir, "yumin.ttf"),    // 游明朝
		filepath.Join(fontsDir, "yumindb.ttf"),  // 游明朝 Demibold

		// その他の日本語フォント
		filepath.Join(fontsDir, "ipag.ttf"), // IPAゴシック
		filepath.Join(fontsDir, "ipam.ttf"), // IPA明朝

		// 英字フォント（フォールバック）
		filepath.Join(fontsDir, "segoeui.ttf"),
		filepath.Join(fontsDir, "segoeuib.ttf"),
		filepath.Join(fontsDir, "arial.ttf"),
		filepath.Join(fontsDir, "arialbd.ttf"),
		filepath.Join(fontsDir, "tahoma.ttf"),
		filepath.Join(fontsDir, "tahomabd.ttf"),
	}

	// ユーザーフォントディレクトリも探索
	if localAppData != "" {
		userFonts := filepath.Join(localAppData, "Microsoft", "Windows", "Fonts")
		paths = append(paths,
			filepath.Join(userFonts, "NotoSansCJKjp-Bold.otf"),
			filepath.Join(userFonts, "NotoSansCJKjp-Regular.otf"),
		)
	}

	return paths
}

// platformGetClipboard gets text from clipboard on Windows
func platformGetClipboard() (string, error) {
	// PowerShellを使用してクリップボードを取得
	cmd := exec.Command("powershell", "-Command", "Get-Clipboard")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("クリップボード取得に失敗: %v", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// platformSetEnvVar sets environment variable on Windows
func platformSetEnvVar(name, value string) bool {
	// 現在のプロセスの環境変数を設定
	os.Setenv(name, value)

	// setxを使用してユーザー環境変数に永続化
	err := exec.Command("setx", name, value).Run()
	return err == nil
}

// platformSpeakText implements text-to-speech on Windows
func platformSpeakText(text string) {
	// Try multiple methods for Windows TTS

	// Method 1: PowerShell SpeechSynthesizer (Windows 8+)
	psScript := fmt.Sprintf(`Add-Type -AssemblyName System.speech; $speak = New-Object System.Speech.Synthesis.SpeechSynthesizer; $speak.Speak("%s")`, strings.ReplaceAll(text, `"`, `\"`))
	cmd := exec.Command("powershell", "-Command", psScript)
	if err := cmd.Start(); err == nil {
		log.Printf("[TTS] 音声合成成功: %s (PowerShell SpeechSynthesizer)", text)
		return
	}

	// Method 2: Using SAPI via VBScript (older Windows)
	vbsScript := fmt.Sprintf(`CreateObject("SAPI.SpVoice").Speak "%s"`, strings.ReplaceAll(text, `"`, `\"`))
	vbsFile := os.TempDir() + "\\tts.vbs"
	os.WriteFile(vbsFile, []byte(vbsScript), 0644)
	defer os.Remove(vbsFile)

	cmd = exec.Command("cscript", "//Nologo", vbsFile)
	if err := cmd.Start(); err == nil {
		log.Printf("[TTS] 音声合成成功: %s (VBScript SAPI)", text)
		return
	}

	// Method 3: Using built-in narrator commands (Windows 10+)
	cmd = exec.Command("cmd", "/c", "echo", text, "|", "clip")
	if err := cmd.Run(); err == nil {
		// Try to use narrator hotkey (Win+Ctrl+Enter) - this is tricky from command line
		log.Printf("[TTS] テキストをクリップボードにコピーしました: %s", text)
		log.Printf("[TTS] Windowsナレーターを使用するには Win+Ctrl+Enter を押してください")
		return
	}

	log.Printf("[TTS] 警告: Windows音声合成機能の実行に失敗しました")
	log.Printf("[TTS] 以下の方法を試してください:")
	log.Printf("[TTS]   1. Windows音声認識機能が有効か確認")
	log.Printf("[TTS]   2. 別のTTSソフトウェアをインストール")
}
