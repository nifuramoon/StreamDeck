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
		return &V2Device{file: file}, nil
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
	exec.Command("shutdown", "/r", "/t", "0").Start()
}

func platformLoadFontPaths() []string {
	winDir := os.Getenv("WINDIR")
	if winDir == "" {
		winDir = `C:\Windows`
	}
	fontsDir := filepath.Join(winDir, "Fonts")
	localAppData := os.Getenv("LOCALAPPDATA")

	paths := []string{
		filepath.Join(fontsDir, "YuGothB.ttc"),
		filepath.Join(fontsDir, "YuGothM.ttc"),
		filepath.Join(fontsDir, "msgothic.ttc"),
		filepath.Join(fontsDir, "meiryo.ttc"),
		filepath.Join(fontsDir, "meiryob.ttc"),
		filepath.Join(fontsDir, "segoeui.ttf"),
		filepath.Join(fontsDir, "arial.ttf"),
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
