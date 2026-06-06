package main

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// --- Device Structure ---
type V2Device struct {
	file       *os.File
	cb         func(int, bool)
	mu         sync.Mutex
	closed     bool
	prevImages []string
	virtualDir string // if set, save PNGs here instead of USB
}

// --- Constants & Config ---
const (
	V2_PAGE_PACKET_SZ = 1024
	V2_ITER_SZ        = 1016
	V2_HEADER_SZ      = 8

	MAX_KEYS        = 15
	MAX_TWITCH_KEYS = 14
	SCROLL_IV       = 0.033
	FETCH_IV        = 3
	IDLE_TIMEOUT    = 60.0
	W               = 72
	H               = 72
)

const (
	HOME, TW, LV, TX, NX, ST, SD, OA = "home", "tw", "lv", "tx", "nx", "st", "sd", "oa"
)

var (
	// グローバル変数（main関数内で初期化）
	CID   string
	CS    string
	SCOPE string
	AT    string
	RT    string
	UID   string
	IRC_T string
)

var EMOTES = []string{"BloodTrail", "HeyGuys", "LUL", "DinoDance", "HungryPaimon", "GlitchCat"}
var DEFAULT_TEXTS = []string{"うおw", "うま", "うっま", "あ", "www", "wwww", "wwwww", "wwwww", "こっから勝・つ・ぞ！オイ！💃", "んん〜まかｧｧウｯｯ!!!!🤏😎", "うおおおおおおおおお", "きたあああああああ", "いいね"}
var DEFAULT_NEXT = []string{"あ）"}

// --- Globals ---
var (
	sdeck      *V2Device
	deckMu     sync.Mutex
	page       = TW
	stack      []stackEntry
	live       string
	brightness = 50
	lastInput  = time.Now()

	stateMu    sync.RWMutex
	followed   []string
	lu         = map[string]map[string]interface{}{}
	id2lg      = map[string]string{}
	twOrder    []string
	views      = map[string]int{}
	startedAt  = map[string]float64{}
	titles     = map[string]string{}
	titleOfs   = map[string]float64{}
	titleStep  = map[string]float64{}
	categories = map[string]string{}
	catOfs     = map[string]float64{}
	catStep    = map[string]float64{}
	titleW     = map[string]float64{}
	catW       = map[string]float64{}

	profCache  = NewLRU(50)
	httpClient = &http.Client{Timeout: 10 * time.Second}

	fontRegular *truetype.Font
	fontSmall   *truetype.Font
	// OpenType font for rendering (replaces broken truetype Glyph)
	otFont     *sfnt.Font
	otFontData []byte // raw font bytes for creating faces

	scrollMode = "title"

	// Cache directories
	profDir         string
	followCachePath string

	// State tracking
	lastOnlineCount int
	titleWrapped    = map[string]bool{}
	catWrapped      = map[string]bool{}

	// Stream notification tracking
	prevOnline          map[string]bool // 前回のオンライン状態
	prevOnlineMu        sync.RWMutex    // prevOnline用のロック
	notificationEnabled bool            // 通知機能の有効/無効

	// Log analyzer for automatic error detection and fixes
	logAnalyzer *LogAnalyzer

	// Token manager for OAuth token handling
	tokenManager *TokenManager

	// Debug mode flag - set to true for verbose logging
	debugMode = false
)

type stackEntry struct{ page, ctx string }

// --- LRU Cache ---
type LRUCache struct {
	mu   sync.Mutex
	data map[string]interface{}
	keys []string
	max  int
}

func NewLRU(max int) *LRUCache { return &LRUCache{data: make(map[string]interface{}), max: max} }
func (c *LRUCache) Get(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.data[key]
	return v, ok
}
func (c *LRUCache) Set(key string, val interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.data[key]; !ok {
		c.keys = append(c.keys, key)
	}
	c.data[key] = val
	if len(c.keys) > c.max {
		delete(c.data, c.keys[0])
		c.keys = c.keys[1:]
	}
}

// Simple Japanese character drawing functions
// These draw simplified representations of common characters

func drawJapaneseChar認(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	// Draw a simple representation of 認
	drawRect(img, x, y, w, h, col)
	// Add distinguishing features
	innerCol := color.RGBA{col.R / 2, col.G / 2, col.B / 2, 255}
	drawLine(img, x+w/4, y+h/4, x+w*3/4, y+h/4, innerCol)
	drawLine(img, x+w/4, y+h/2, x+w*3/4, y+h/2, innerCol)
}

func drawJapaneseChar証(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	drawRect(img, x, y, w, h, col)
	innerCol := color.RGBA{col.R / 2, col.G / 2, col.B / 2, 255}
	drawLine(img, x+w/4, y+h/4, x+w/4, y+h*3/4, innerCol)
	drawLine(img, x+w*3/4, y+h/4, x+w*3/4, y+h*3/4, innerCol)
}

func drawJapaneseChar取(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	drawRect(img, x, y, w, h, col)
	innerCol := color.RGBA{col.R / 2, col.G / 2, col.B / 2, 255}
	drawLine(img, x+w/4, y+h/4, x+w*3/4, y+h/4, innerCol)
	drawLine(img, x+w/2, y+h/4, x+w/2, y+h*3/4, innerCol)
}

func drawJapaneseChar得(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	drawRect(img, x, y, w, h, col)
	innerCol := color.RGBA{col.R / 2, col.G / 2, col.B / 2, 255}
	// Draw a diagonal cross
	drawLine(img, x+w/4, y+h/4, x+w*3/4, y+h*3/4, innerCol)
	drawLine(img, x+w*3/4, y+h/4, x+w/4, y+h*3/4, innerCol)
}

func drawJapaneseChar保(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	drawRect(img, x, y, w, h, col)
	innerCol := color.RGBA{col.R / 2, col.G / 2, col.B / 2, 255}
	// Draw vertical line with horizontal bars
	drawLine(img, x+w/2, y+h/4, x+w/2, y+h*3/4, innerCol)
	drawLine(img, x+w/4, y+h/3, x+w*3/4, y+h/3, innerCol)
	drawLine(img, x+w/4, y+h*2/3, x+w*3/4, y+h*2/3, innerCol)
}

func drawJapaneseChar存(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	drawRect(img, x, y, w, h, col)
	innerCol := color.RGBA{col.R / 2, col.G / 2, col.B / 2, 255}
	// Draw a triangle-like shape
	drawLine(img, x+w/2, y+h/4, x+w/4, y+h*3/4, innerCol)
	drawLine(img, x+w/2, y+h/4, x+w*3/4, y+h*3/4, innerCol)
	drawLine(img, x+w/4, y+h*3/4, x+w*3/4, y+h*3/4, innerCol)
}

func drawJapaneseChar戻(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	drawRect(img, x, y, w, h, col)
	innerCol := color.RGBA{col.R / 2, col.G / 2, col.B / 2, 255}
	// Draw an arrow-like shape (return symbol)
	drawLine(img, x+w/4, y+h/2, x+w*3/4, y+h/2, innerCol)
	drawLine(img, x+w/4, y+h/2, x+w/2, y+h/4, innerCol)
	drawLine(img, x+w/4, y+h/2, x+w/2, y+h*3/4, innerCol)
}

func drawJapaneseCharる(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	drawRect(img, x, y, w, h, col)
	innerCol := color.RGBA{col.R / 2, col.G / 2, col.B / 2, 255}
	// Draw a curved shape for hiragana る
	drawLine(img, x+w/4, y+h/4, x+w*3/4, y+h/4, innerCol)
	drawLine(img, x+w*3/4, y+h/4, x+w*3/4, y+h*3/4, innerCol)
	drawLine(img, x+w/4, y+h*3/4, x+w*3/4, y+h*3/4, innerCol)
}

func drawJapaneseChar日(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	drawRect(img, x, y, w, h, col)
	innerCol := color.RGBA{col.R / 2, col.G / 2, col.B / 2, 255}
	// Draw a rectangle with a line in the middle (sun/day character)
	drawRectOutline(img, x+w/4, y+h/4, w/2, h/2, innerCol)
	drawLine(img, x+w/4, y+h/2, x+w*3/4, y+h/2, innerCol)
}

func drawJapaneseChar本(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	drawRect(img, x, y, w, h, col)
	innerCol := color.RGBA{col.R / 2, col.G / 2, col.B / 2, 255}
	// Draw a tree-like shape (book/main character)
	drawLine(img, x+w/2, y+h/4, x+w/2, y+h*3/4, innerCol)
	drawLine(img, x+w/4, y+h/2, x+w*3/4, y+h/2, innerCol)
	drawLine(img, x+w/4, y+h*3/4, x+w*3/4, y+h*3/4, innerCol)
}

func drawJapaneseChar語(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	drawRect(img, x, y, w, h, col)
	innerCol := color.RGBA{col.R / 2, col.G / 2, col.B / 2, 255}
	// Draw a speech/language symbol
	drawLine(img, x+w/4, y+h/4, x+w*3/4, y+h/4, innerCol)
	drawLine(img, x+w/4, y+h/2, x+w*3/4, y+h/2, innerCol)
	drawLine(img, x+w/4, y+h*3/4, x+w*3/4, y+h*3/4, innerCol)
	drawLine(img, x+w/4, y+h/4, x+w/4, y+h*3/4, innerCol)
}

func (s *V2Device) ClearAllBtns() {
	for i := 0; i < MAX_KEYS; i++ {
		s.FillBlank(i)
	}
}
func (s *V2Device) Close() {
	s.closed = true
	if s.file != nil {
		s.file.Close()
	}
}
func (s *V2Device) FillBlank(idx int) {
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{0, 0, 0, 255}), image.Point{}, draw.Src)
	s.FillImage(idx, img)
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
func (s *V2Device) FillImage(idx int, img image.Image) {
	if s.virtualDir != "" {
		dst := image.NewRGBA(image.Rect(0, 0, 72, 72))
		r, g, b, a := img.At(0, 0).RGBA()
		log.Printf("[Virtual FillImage] btn%d img.At(0,0)=RGBA(%d,%d,%d,%d)", idx, r/257, g/257, b/257, a/257)
		// Use the actual image color
		for y := 0; y < 72; y++ {
			for x := 0; x < 72; x++ {
				rr, gg, bb, aa := img.At(x, y).RGBA()
				off := dst.PixOffset(x, y)
				dst.Pix[off+0] = uint8(rr / 257)
				dst.Pix[off+1] = uint8(gg / 257)
				dst.Pix[off+2] = uint8(bb / 257)
				dst.Pix[off+3] = uint8(aa / 257)
			}
		}
		// Also check after copy
		r2, g2, b2, _ := dst.At(0, 0).RGBA()
		log.Printf("[Virtual FillImage] btn%d dst.At(0,0)=RGBA(%d,%d,%d)", idx, r2/257, g2/257, b2/257)
		fname := filepath.Join(s.virtualDir, fmt.Sprintf("btn%d.png", idx))
		f, _ := os.Create(fname)
		if f != nil {
			png.Encode(f, dst)
			f.Close()
			log.Printf("[Virtual] Saved button %d to %s", idx, fname)
		}
		return
	}

	flipped := flipV2(img)
	var buf bytes.Buffer
	jpeg.Encode(&buf, flipped, &jpeg.Options{Quality: 90})
	payload := buf.Bytes()
	hash := fmt.Sprintf("%x", sha1.Sum(payload))
	if s.prevImages[idx] == hash {
		return
	}
	s.prevImages[idx] = hash

	pageNum, sent := 0, 0
	for sent < len(payload) {
		chunkSz := len(payload) - sent
		if chunkSz > V2_ITER_SZ {
			chunkSz = V2_ITER_SZ
		}
		isLast := 0
		if sent+chunkSz == len(payload) {
			isLast = 1
		}
		header := make([]byte, V2_HEADER_SZ)
		header[0], header[1], header[2], header[3] = 0x02, 0x07, byte(idx), byte(isLast)
		header[4], header[5] = byte(chunkSz&0xFF), byte(chunkSz>>8)
		header[6], header[7] = byte(pageNum&0xFF), byte(pageNum>>8)
		packet := make([]byte, V2_PAGE_PACKET_SZ)
		copy(packet, header)
		copy(packet[V2_HEADER_SZ:], payload[sent:sent+chunkSz])
		s.file.Write(packet)
		sent += chunkSz
		pageNum++
	}
}
func (s *V2Device) readLoop() {
	prev := make([]byte, MAX_KEYS)
	for !s.closed {
		buf := make([]byte, 32)
		n, err := s.file.Read(buf)
		if err != nil || n < 4 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if buf[0] == 0x01 {
			current := buf[4 : 4+MAX_KEYS]
			for i := 0; i < MAX_KEYS; i++ {
				if current[i] != prev[i] && s.cb != nil {
					s.cb(i, current[i] == 1)
				}
			}
			copy(prev, current)
		}
	}
}
func (s *V2Device) SetBrightness(percent int) {
	payload := make([]byte, 32)
	payload[0], payload[1], payload[2] = 0x03, 0x08, byte(percent)
	if err := platformSetBrightness(s.file.Fd(), payload); err != nil {
		log.Printf("[Brightness] error: %v", err)
	}
}

// --- Entry Point ---
func init() {
	userCache, err := os.UserCacheDir()
	if err != nil {
		userCache = os.TempDir()
	}

	cacheDir := filepath.Join(userCache, "streamdeck-twitch")
	profDir = filepath.Join(cacheDir, "profiles")
	followCachePath = filepath.Join(cacheDir, "followed.json")
	os.MkdirAll(profDir, 0755)

	log.Printf("[INFO] キャッシュディレクトリ: %s\n", cacheDir)
}

func main() {
	// Initialize global variables with proper priority:
	// 1. Environment variables
	// 2. Config file
	// 3. Default values
	CID = getEnvWithDefault("TWITCH_CLIENT_ID", "")
	CS = getEnvWithDefault("TWITCH_CLIENT_SECRET", "")
	SCOPE = getEnvWithDefault("TWITCH_SCOPE", "user:read:email user:read:follows user:read:broadcast user:write:chat chat:read")
	AT = os.Getenv("TWITCH_ACCESS_TOKEN")
	RT = os.Getenv("TWITCH_REFRESH_TOKEN")
	UID = os.Getenv("TWITCH_USER_ID")
	IRC_T = os.Getenv("TWITCH_IRC_TOKEN")

	// Auto-fix mode check
	if len(os.Args) > 1 && os.Args[1] == "--auto-fix" {
		infoLog("自動修正モードで起動")
		if !RunWithAutoFix() {
			errorLog("自動修正モードで起動失敗")
			os.Exit(1)
		}
		return
	}

	// Check and load configuration file
	if !checkAndSetupConfig() {
		errorLog("Configuration not complete. Edit config file and restart.")
		os.Exit(1)
	}

	// Initialize log analyzer for error monitoring
	logAnalyzer = NewLogAnalyzer()
	log.Println("[LOG ANALYZER] Log monitoring started")

	// Initialize token manager
	tokenManager = NewTokenManager()

	// Try to load existing token
	if token, err := tokenManager.LoadToken(); err == nil {
		// Validate the token
		if valid, reason := tokenManager.ValidateToken(); valid {
			// Set global variables from token
			AT = token.AccessToken
			RT = token.RefreshToken
			UID = token.UserID
			CID = token.ClientID
			// DO NOT update SCOPE from token - keep config.json scope
			// SCOPE = token.Scope  // COMMENTED OUT - preserve config scope

			log.Printf("[Token] Using valid token for user: %s (%s)", token.DisplayName, token.LoginName)
			log.Printf("[Token Debug] Token scope: %s (not updating global SCOPE)", token.Scope)
			log.Printf("[Token Debug] Config scope (preserved): %s", SCOPE)
			tokenManager.UpdateLastUsed()
		} else {
			log.Printf("[Token] Token validation failed: %s", reason)
			logAnalyzer.LogTokenError("VALIDATION_FAILED", reason)
			log.Println("[Token] Please re-authenticate using OAuth button")
			AT = "" // Clear invalid token
		}
	}

	// After token manager load, ensure SCOPE is set from config.json, not token
	// Reload config to get the correct scope
	config := loadConfigFromFile()
	if config.Scope != "" {
		SCOPE = config.Scope
		log.Printf("[Config] Reset SCOPE from config after token load: %s", SCOPE)
	}

	loadFonts()

	// Virtual mode: render button images to PNG files for testing without device
	if len(os.Args) > 1 && os.Args[1] == "--virtual" {
		infoLog("仮想モード起動（デバイスなし）")
		outDir := "/home/nifuramu/Desktop/streamdeck_test"
		os.RemoveAll(outDir)
		os.MkdirAll(outDir, 0755)
		sdeck = &V2Device{
			virtualDir: outDir,
			prevImages: make([]string, MAX_KEYS),
		}
		page := HOME
		if len(os.Args) > 2 {
			switch os.Args[2] {
			case "tw": page = TW
			case "home": page = HOME
			}
		}
		show(page, "", false)
		infoLog("ページ %s のテスト画像を %s に出力しました", page, outDir)
		return
	}

	var err error
	if sdeck, err = openStreamDeck(); err != nil {
		log.Fatalf("[ERROR] Stream Deck: %v", err)
	}
	defer sdeck.Close()

	sdeck.cb = func(idx int, pressed bool) {
		if pressed {
			onKey(idx, true)
		}
	}
	go sdeck.readLoop()

	sdeck.SetBrightness(brightness)

	// Start from HOME page if no Access Token, otherwise start from TWITCH page
	if AT == "" {
		show(HOME, "", false)
		logAnalyzer.LogError("NO_TOKEN", "Access Token not set at startup")
		log.Println("[INFO] Access Token not set. Start authentication from OAuth button on HOME page.")
	} else {
		show(TW, "", false)
	}

	// まずキャッシュからフォローリストを読み込み
	cachedFollows := loadFollowedFromCache()

	// APIからフォローリストを取得
	apiFollows := fetchFollowedFromAPI()

	if len(apiFollows) > 0 {
		followed = apiFollows
		// APIから取得したらキャッシュに保存
		saveFollowedToCache(apiFollows)
	} else if len(cachedFollows) > 0 {
		// APIが失敗したらキャッシュを使用
		followed = cachedFollows
		log.Println("[Cache] APIからフォローリストを取得できなかったため、キャッシュを使用します")
	} else {
		// どちらもない場合はデフォルト
		followed = []string{"hanjoudesu", "bijusan", "oniyadayo", "dmf_kyochan", "vodkavdk", "lazvell", "ade3_3", "goroujp", "batora324", "kato_junichi0817", "crowfps__", "gon_vl", "yuyuta0702"}
		log.Println("[Cache] キャッシュもAPIも利用できないため、デフォルトのフォローリストを使用します")
	}

	fetchUsers(followed)

	// 通知機能の初期化
	prevOnline = loadPrevOnlineState()
	notificationEnabled = loadNotificationSetting()

	go bgLoop()
	go ircLoop()
	mainLoop()
}

// --- Utils ---
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Debug logging functions
func debugLog(format string, args ...interface{}) {
	if debugMode {
		log.Printf("[DEBUG] "+format, args...)
	}
}

func infoLog(format string, args ...interface{}) {
	log.Printf("[INFO] "+format, args...)
}

func warnLog(format string, args ...interface{}) {
	log.Printf("[WARN] "+format, args...)
}

func errorLog(format string, args ...interface{}) {
	log.Printf("[ERROR] "+format, args...)
}

// --- Graphics ---
func tryDrawWithOpenType(img *image.RGBA, x, y int, text string, col color.RGBA, size float64) bool {
	candidates := platformLoadFontPaths()
	for _, p := range candidates {
		isJapaneseFont := strings.Contains(strings.ToLower(p), "ipa") ||
			strings.Contains(strings.ToLower(p), "noto") ||
			strings.Contains(strings.ToLower(p), "japanese") ||
			strings.Contains(strings.ToLower(p), "cjk") ||
			strings.Contains(strings.ToLower(p), "jp")
		ext := strings.ToLower(filepath.Ext(p))
		if (ext == ".ttf" || ext == ".otf" || ext == ".ttc") && isJapaneseFont {
			if data, err := os.ReadFile(p); err == nil {
				var sf *sfnt.Font
				if ext == ".ttc" {
					if coll, e := sfnt.ParseCollection(data); e == nil {
						sf, err = coll.Font(0)
					}
				} else {
					sf, err = sfnt.Parse(data)
				}
				if err != nil || sf == nil { continue }
				face, err := opentype.NewFace(sf, &opentype.FaceOptions{
					Size: size, DPI: 72, Hinting: font.HintingFull,
				})
				if err != nil { continue }
				defer face.Close()

				canRender := true
				if len(text) > 0 {
					r := []rune(text)[0]
					advance, ok := face.GlyphAdvance(r)
					if !ok || advance == 0 { canRender = false }
				}
				if canRender {
					metrics := face.Metrics()
					ascent := metrics.Ascent.Ceil()
					if ascent == 0 { ascent = int(size * 0.8) }
					d := &font.Drawer{
						Dst: img, Src: image.NewUniform(col), Face: face,
						Dot: fixed.P(x, y+ascent),
					}
					d.DrawString(text)
					debugLog("[Font Debug] Drew with opentype: %s", filepath.Base(p))
					return true
				}
			}
		}
	}

	// If no Japanese font worked, try any font
	for _, p := range candidates {
		if strings.HasSuffix(strings.ToLower(p), ".ttf") || strings.HasSuffix(strings.ToLower(p), ".otf") {
			if data, err := os.ReadFile(p); err == nil {
				if f, err := opentype.Parse(data); err == nil {
					face, err := opentype.NewFace(f, &opentype.FaceOptions{
						Size:    size,
						DPI:     72,
						Hinting: font.HintingFull,
					})
					if err != nil {
						continue
					}
					defer face.Close()

					metrics := face.Metrics()
					ascent := metrics.Ascent.Ceil()
					if ascent == 0 {
						ascent = int(size * 0.8)
					}

					d := &font.Drawer{
						Dst:  img,
						Src:  image.NewUniform(col),
						Face: face,
						Dot:  fixed.P(x, y+ascent),
					}

					d.DrawString(text)
					return true
				}
			}
		}
	}
	return false
}

// drawSimpleText draws text using simple rectangles when no font is available
type bitmap struct { w, h int; data []uint8 }

func onKey(k int, p bool) {
	lastInput = time.Now()

	// Log button press for analysis
	if logAnalyzer != nil && p {
		buttonLabel := getButtonLabel(page, k)
		logAnalyzer.LogButtonPress(page, k, buttonLabel)
	}

	switch page {
	case HOME:
		if k == 0 {
			show(TW, "", true)
		} else if k == 1 {
			show(OA, "", true)
		} else if k == 2 {
			show(ST, "", true)
		}
	case TW:
		if k == 14 {
			show(HOME, "", false)
		} else if k < len(twOrder) {
			show(LV, twOrder[k], true)
		}
	case LV:
		if k < len(EMOTES) && live != "" {
			ircSend(live, EMOTES[k])
		}
		if k == 11 && live != "" {
			platformOpenBrowser("https://www.twitch.tv/" + live)
		}
		if k == 12 {
			show(TX, live, true)
		}
		if k == 13 {
			show(HOME, "", false)
		}
		if k == 14 {
			back()
		}
	case TX:
		if k < len(DEFAULT_TEXTS) && live != "" {
			ircSend(live, DEFAULT_TEXTS[k])
		}
		if k == 12 {
			show(NX, live, true)
		}
		if k == 13 {
			show(HOME, "", false)
		}
		if k == 14 {
			back()
		}
	case NX:
		if k < len(DEFAULT_NEXT) && live != "" {
			ircSend(live, DEFAULT_NEXT[k])
		}
		if k == 13 {
			show(HOME, "", false)
		}
		if k == 14 {
			back()
		}
	case ST:
		if k == 0 {
			show(SD, "", true)
		} else if k == 1 {
			platformReboot()
		} else if k == 2 {
			// 通知設定の切り替え
			notificationEnabled = !notificationEnabled
			if notificationEnabled {
				log.Println("[設定] 配信開始通知を有効にしました")
				// 現在オンラインの配信者をprevOnlineに登録し、既存配信の通知を防止
				prevOnlineMu.Lock()
				for _, lg := range twOrder {
					prevOnline[lg] = true
				}
				prevOnlineMu.Unlock()
				go speakText("通知をオンにしました")
			} else {
				log.Println("[設定] 配信開始通知を無効にしました")
				go speakText("通知をオフにしました")
			}
			// 設定をファイルに保存
			saveNotificationSetting(notificationEnabled)
			// 設定画面を再描画（即時更新）
			renderST()
		} else if k == 3 {
			log.Println("[設定] テスト音声を再生します")
			go platformSpeakText("テスト音声です")
		}
		if k == 14 {
			show(HOME, "", false)
		}
	case SD:
		if k == 0 {
			brightness = min(100, brightness+10)
			sdeck.SetBrightness(brightness)
			renderSD()
		} else if k == 1 {
			brightness = max(0, brightness-10)
			sdeck.SetBrightness(brightness)
			renderSD()
		}
		if k == 13 {
			show(HOME, "", false)
		} else if k == 14 {
			back()
		}
	case OA:
		if k == 0 {
			startOAuth()
		} else if k == 14 {
			back()
		}
	}
}

// getButtonLabel returns the label for a button based on page and index
func getButtonLabel(page string, buttonIndex int) string {
	switch page {
	case HOME:
		switch buttonIndex {
		case 0:
			return "Twitch"
		case 1:
			return "OAuth"
		case 2:
			return "Setting"
		default:
			return fmt.Sprintf("Button %d", buttonIndex)
		}
	case OA:
		switch buttonIndex {
		case 0:
			return "Auth"
		case 14:
			return "Back"
		default:
			return fmt.Sprintf("Button %d", buttonIndex)
		}
	case TW:
		if buttonIndex == 14 {
			return "Back"
		}
		return fmt.Sprintf("Streamer %d", buttonIndex)
	case LV:
		if buttonIndex < len(EMOTES) {
			return EMOTES[buttonIndex]
		}
		return fmt.Sprintf("Button %d", buttonIndex)
	case ST:
		switch buttonIndex {
		case 0:
			return "StreamDeck"
		case 1:
			return "再起動"
		case 14:
			return "ホーム"
		default:
			return "" // ボタン2-13は空白
		}
	case SD:
		switch buttonIndex {
		case 0:
			return "明るさUP"
		case 1:
			return "明るさDW"
		case 13:
			return "ホーム"
		case 14:
			return "戻る"
		default:
			return fmt.Sprintf("Button %d", buttonIndex)
		}
	default:
		return fmt.Sprintf("Page:%s Btn:%d", page, buttonIndex)
	}
}

// showTokenError displays a token error message to the user
func showTokenError(message string) {
	log.Printf("[TOKEN ERROR] %s", message)

	// Log error to analyzer
	if logAnalyzer != nil {
		logAnalyzer.LogTokenError("ERROR", message)
	}

	// Switch to HOME page to show OAuth button
	if page != HOME {
		show(HOME, "", false)
		log.Println("[INFO] Please use OAuth button to re-authenticate")
	}
}

// --- IRC ---
var (
	ircConn          net.Conn
	ircMu            sync.Mutex
	ircJoined        = make(map[string]bool) // 参加済みチャンネル
	ircUsername      = ""                    // IRCユーザー名
	ircUsernameTries = 0                     // ユーザー名取得試行回数
	lastPing         time.Time               // 最後にPINGを送信した時間
)

// fetchIRCUsername fetches the Twitch username from the API
// Returns true if successful, false otherwise