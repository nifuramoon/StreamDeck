package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// --- Device Structure ---
type V2Device struct {
	file       *os.File
	cb         func(int, bool)
	mu         sync.Mutex
	closed     bool
	prevImages []string
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
	// デフォルト値（ユーザーが環境変数や設定ファイルで上書き可能）
	CID   = getEnvWithDefault("TWITCH_CLIENT_ID", "zl3bbnc9ja0mdawfba3rar9jokjb0f")
	CS    = getEnvWithDefault("TWITCH_CLIENT_SECRET", "vo9ks19oyb8x2uha040245pj9s2klv")
	SCOPE = getEnvWithDefault("TWITCH_SCOPE", "user:read:email user:read:follows user:read:broadcast user:write:chat chat:read")
	AT    = os.Getenv("TWITCH_ACCESS_TOKEN")
	RT    = os.Getenv("TWITCH_REFRESH_TOKEN")
	UID   = os.Getenv("TWITCH_USER_ID")
	IRC_T = os.Getenv("TWITCH_IRC_TOKEN")
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

	scrollMode = "title"

	// Cache directories
	profDir         string
	followCachePath string

	// State tracking
	lastOnlineCount int
	titleWrapped    = map[string]bool{}
	catWrapped      = map[string]bool{}

	// Log analyzer for automatic error detection and fixes
	logAnalyzer *LogAnalyzer

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
			SCOPE = token.Scope

			log.Printf("[Token] Using valid token for user: %s (%s)", token.DisplayName, token.LoginName)
			tokenManager.UpdateLastUsed()
		} else {
			log.Printf("[Token] Token validation failed: %s", reason)
			logAnalyzer.LogTokenError("VALIDATION_FAILED", reason)
			log.Println("[Token] Please re-authenticate using OAuth button")
			AT = "" // Clear invalid token
		}
	}

	loadFonts()
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
func hsh(s string) string { return fmt.Sprintf("%x", sha1.Sum([]byte(s))) }
func newImg() *image.RGBA { return image.NewRGBA(image.Rect(0, 0, W, H)) }
func fillRect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Src)
}

// formatViewerCount formats viewer count with "k" suffix
func formatViewerCount(count int) string {
	if count >= 1000 {
		// 1.5k形式で表示
		k := float64(count) / 1000.0
		return fmt.Sprintf("%.1fk", k)
	}
	return strconv.Itoa(count)
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func resize72(src image.Image) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, 72, 72))
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < 72; y++ {
		for x := 0; x < 72; x++ {
			dst.Set(x, y, src.At(src.Bounds().Min.X+(x*w/72), src.Bounds().Min.Y+(y*h/72)))
		}
	}
	return dst
}

func loadFonts() {
	candidates := platformLoadFontPaths()
	debugLog("Searching for fonts in %d paths...", len(candidates))

	// まず.ttfファイルを探す（.ttcファイルより優先）
	ttfFiles := []string{}
	ttcFiles := []string{}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			if strings.HasSuffix(strings.ToLower(p), ".ttf") {
				ttfFiles = append(ttfFiles, p)
			} else if strings.HasSuffix(strings.ToLower(p), ".ttc") || strings.HasSuffix(strings.ToLower(p), ".otf") {
				ttcFiles = append(ttcFiles, p)
			}
		}
	}

	// .ttfファイルを先に試す
	allFiles := append(ttfFiles, ttcFiles...)

	// 方法1: opentypeパッケージで試す（より良いサポート）
	for i, p := range allFiles {
		if data, err := os.ReadFile(p); err == nil {
			// Try opentype first (better support)
			if f, err := opentype.Parse(data); err == nil {
				// Create face with appropriate size
				face, err := opentype.NewFace(f, &opentype.FaceOptions{
					Size:    14,
					DPI:     72,
					Hinting: font.HintingFull,
				})
				if err == nil {
					// We need to store the parsed font for later use
					// For now, use freetype to store the font
					if tf, err := freetype.ParseFont(data); err == nil {
						fontRegular, fontSmall = tf, tf
						infoLog("Loaded font with opentype: %s (file %d/%d)", filepath.Base(p), i+1, len(allFiles))

						// デバッグモード時のみ詳細情報を表示
						if debugMode {
							if name := tf.Name(truetype.NameIDFontFullName); name != "" {
								debugLog("Font name (Full): %s", name)
							}
							if family := tf.Name(truetype.NameIDFontFamily); family != "" {
								debugLog("Font family: %s", family)
							}
							if subfamily := tf.Name(truetype.NameIDFontSubfamily); subfamily != "" {
								debugLog("Font subfamily: %s", subfamily)
							}
						}

						// Check if this is a Japanese font
						fontName := tf.Name(truetype.NameIDFontFullName)
						if strings.Contains(strings.ToLower(fontName), "japanese") ||
							strings.Contains(strings.ToLower(fontName), "jp") ||
							strings.Contains(strings.ToLower(fontName), "cjk") ||
							strings.Contains(strings.ToLower(p), "ipa") ||
							strings.Contains(strings.ToLower(p), "noto") {
							log.Printf("[Font] ✅ Japanese font detected: %s", fontName)
						}

						// Test Japanese character support
						testJapaneseText(face)
						face.Close()
						return
					}
					face.Close()
				}
			}
		}
	}

	// 方法2: 従来のfreetypeで試す
	for i, p := range allFiles {
		if data, err := os.ReadFile(p); err == nil {
			if f, err := freetype.ParseFont(data); err == nil {
				fontRegular, fontSmall = f, f
				log.Printf("[Font] ✅ Loaded with freetype: %s (file %d/%d)", p, i+1, len(allFiles))

				// フォント名を詳細に確認
				if name := f.Name(truetype.NameIDFontFullName); name != "" {
					log.Printf("[Font] Font name (Full): %s", name)
				}
				if family := f.Name(truetype.NameIDFontFamily); family != "" {
					log.Printf("[Font] Font family: %s", family)
				}
				if subfamily := f.Name(truetype.NameIDFontSubfamily); subfamily != "" {
					log.Printf("[Font] Font subfamily: %s", subfamily)
				}

				// フォントの情報を確認
				log.Printf("[Font] Font index: %d", f.FUnitsPerEm())

				return
			} else {
				// .ttcファイルの場合は別の方法を試す
				if strings.HasSuffix(strings.ToLower(p), ".ttc") {
					log.Printf("[Font] ⚠️  TTC file may need special handling: %s", p)
					// TTCファイルから最初のフォントを抽出してみる
					if tryLoadFirstFontFromTTC(p) {
						return
					}
				} else {
					log.Printf("[Font] ❌ Failed to parse font: %s (error: %v)", p, err)
				}
			}
		}
	}

	warnLog("No suitable font found. Text rendering may not work.")
	warnLog("Installing fonts may help: Ubuntu/Debian: sudo apt install fonts-liberation")

	// Fallback: use built-in font rendering
	infoLog("Using built-in fallback font rendering (simple rectangles)")
}

// testJapaneseText tests if the font can render Japanese characters
func testJapaneseText(face font.Face) {
	testStrings := []string{"認", "証", "取", "得", "保", "存", "戻", "る", "日", "本", "語", "Auth", "Get", "Save", "Back"}

	supportedCount := 0
	totalCount := len(testStrings)

	for _, s := range testStrings {
		if len(s) == 0 {
			continue
		}
		r := []rune(s)[0]
		advance, ok := face.GlyphAdvance(r)
		if ok && advance > 0 {
			supportedCount++
			log.Printf("[Font Test] Character '%s' (U+%04X): ✅ supported, advance=%v", s, r, advance)
		} else {
			log.Printf("[Font Test] Character '%s' (U+%04X): ❌ NOT SUPPORTED", s, r)
		}
	}

	// Calculate support percentage
	supportPercent := (float64(supportedCount) / float64(totalCount)) * 100
	log.Printf("[Font Test] Support summary: %d/%d characters (%.1f%%)", supportedCount, totalCount, supportPercent)

	if supportPercent < 50 {
		log.Printf("[Font Test] ⚠️  Font has limited Japanese support. Trying next font...")
	}
}

// tryLoadFirstFontFromTTC tries to load the first font from a TrueType Collection
func tryLoadFirstFontFromTTC(path string) bool {
	// Try to load TTC file using a different approach
	log.Printf("[Font] Attempting to load TTC file: %s", path)

	// Read the TTC file
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[Font] ❌ Failed to read TTC file: %v", err)
		return false
	}

	// Try to parse as a collection first
	// Note: freetype doesn't support TTC directly, but we can try to find the first font
	// TTC files have a "ttcf" header
	if len(data) >= 12 && string(data[:4]) == "ttcf" {
		log.Printf("[Font] ✅ Detected TTC file with 'ttcf' header")

		// Try to extract the first font offset
		// TTC header format: "ttcf" (4 bytes), version (4 bytes), numFonts (4 bytes), offsets[numFonts] (each 4 bytes)
		if len(data) >= 12 {
			numFonts := int(binary.BigEndian.Uint32(data[8:12]))
			log.Printf("[Font] TTC contains %d fonts", numFonts)

			if numFonts > 0 && len(data) >= 12+4*numFonts {
				firstOffset := binary.BigEndian.Uint32(data[12:16])
				if int(firstOffset) < len(data) {
					// Try to parse the first font
					fontData := data[firstOffset:]
					if f, err := freetype.ParseFont(fontData); err == nil {
						fontRegular, fontSmall = f, f
						log.Printf("[Font] ✅ Successfully loaded first font from TTC collection")
						if name := f.Name(truetype.NameIDFontFullName); name != "" {
							log.Printf("[Font] Font name: %s", name)
						}
						return true
					} else {
						log.Printf("[Font] ❌ Failed to parse first font in TTC: %v", err)
					}
				}
			}
		}
	}

	// If that doesn't work, try parsing the whole file as a regular font
	// (some TTC files might be parseable as single fonts)
	if f, err := freetype.ParseFont(data); err == nil {
		fontRegular, fontSmall = f, f
		log.Printf("[Font] ✅ Loaded TTC file as single font")
		if name := f.Name(truetype.NameIDFontFullName); name != "" {
			log.Printf("[Font] Font name: %s", name)
		}
		return true
	}

	log.Printf("[Font] ❌ Failed to load TTC file using any method")
	return false
}
func measureText(text string, size float64) int {
	if fontRegular == nil {
		// Approximate width for simple text rendering
		return len(text) * int(size*0.6)
	}
	face := truetype.NewFace(fontRegular, &truetype.Options{Size: size, DPI: 72})
	defer face.Close()
	return font.MeasureString(face, text).Ceil()
}
func drawText(img *image.RGBA, x, y int, text string, col color.RGBA, size float64) {
	if fontRegular == nil {
		// Fallback: draw simple rectangles for characters
		drawSimpleText(img, x, y, text, col, size)
		return
	}

	// Check if text contains Japanese characters
	containsJapanese := false
	for _, r := range text {
		// Check for CJK characters (Japanese, Chinese, Korean)
		if (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
			(r >= 0x3040 && r <= 0x309F) || // Hiragana
			(r >= 0x30A0 && r <= 0x30FF) || // Katakana
			(r >= 0xFF00 && r <= 0xFFEF) { // Halfwidth and Fullwidth Forms
			containsJapanese = true
			break
		}
	}

	// Try multiple approaches for rendering

	// アプローチ1: opentypeを使用（より良い日本語サポート）
	if containsJapanese && tryDrawWithOpenType(img, x, y, text, col, size) {
		log.Printf("[Font Debug] Japanese text drawn with opentype: '%s'", text)
		return
	}

	// アプローチ2: 従来のtruetypeを使用
	face := truetype.NewFace(fontRegular, &truetype.Options{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	defer face.Close()

	// Test if font can render the text
	canRender := true
	if containsJapanese {
		// Test first character
		if len(text) > 0 {
			r := []rune(text)[0]
			advance, ok := face.GlyphAdvance(r)
			if !ok || advance == 0 {
				canRender = false
				log.Printf("[Font Debug] Font cannot render Japanese character: U+%04X '%c'", r, r)
			}
		}
	}

	if !canRender {
		// Fallback to simple text rendering
		drawSimpleText(img, x, y, text, col, size)
		return
	}

	// デバッグ: 描画するテキストをログに出力
	if strings.ContainsAny(text, "AuthGetSaveBack") || containsJapanese {
		log.Printf("[Font Debug] Drawing text: '%s' (Japanese: %v)", text, containsJapanese)
	}

	// Create a drawer
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

	if strings.ContainsAny(text, "AuthGetSaveBack") || containsJapanese {
		bounds, _ := d.BoundString(text)
		log.Printf("[Font Debug] Text bounds: %v (ascent: %d)", bounds, ascent)
	}
}

// tryDrawWithOpenType attempts to draw text using opentype package
func tryDrawWithOpenType(img *image.RGBA, x, y int, text string, col color.RGBA, size float64) bool {
	// We need to reload the font with opentype
	// This is inefficient but works for testing
	candidates := platformLoadFontPaths()

	// First try Japanese fonts
	for _, p := range candidates {
		// Check if this is likely a Japanese font
		isJapaneseFont := strings.Contains(strings.ToLower(p), "ipa") ||
			strings.Contains(strings.ToLower(p), "noto") ||
			strings.Contains(strings.ToLower(p), "japanese") ||
			strings.Contains(strings.ToLower(p), "cjk") ||
			strings.Contains(strings.ToLower(p), "jp")

		if (strings.HasSuffix(strings.ToLower(p), ".ttf") || strings.HasSuffix(strings.ToLower(p), ".otf")) && isJapaneseFont {
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

					// Test if this font can render Japanese
					canRender := true
					if len(text) > 0 {
						r := []rune(text)[0]
						advance, ok := face.GlyphAdvance(r)
						if !ok || advance == 0 {
							canRender = false
						}
					}

					if canRender {
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
						log.Printf("[Font Debug] Drew Japanese text with opentype from: %s", filepath.Base(p))
						return true
					}
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
func drawSimpleText(img *image.RGBA, x, y int, text string, col color.RGBA, size float64) {
	// Simple fallback: draw rectangles for each character
	charWidth := int(size * 0.6) // Narrower for Latin characters
	charHeight := int(size * 0.8)

	for i := 0; i < len(text); i++ {
		if len(text) > 20 && i >= 20 {
			// Draw ellipsis
			drawRect(img, x+i*charWidth, y, 3, charHeight, col)
			drawRect(img, x+i*charWidth+6, y, 3, charHeight, col)
			break
		}

		charX := x + i*charWidth

		// Draw outline for Latin characters (simpler, works for English)
		drawRectOutline(img, charX, y, charWidth, charHeight, col)
	}
}

// drawLine draws a line between two points
func drawLine(img *image.RGBA, x1, y1, x2, y2 int, col color.RGBA) {
	dx := abs(x2 - x1)
	dy := abs(y2 - y1)
	sx := -1
	if x1 < x2 {
		sx = 1
	}
	sy := -1
	if y1 < y2 {
		sy = 1
	}
	err := dx - dy

	for {
		if x1 >= 0 && x1 < img.Bounds().Dx() && y1 >= 0 && y1 < img.Bounds().Dy() {
			img.Set(x1, y1, col)
		}
		if x1 == x2 && y1 == y2 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x1 += sx
		}
		if e2 < dx {
			err += dx
			y1 += sy
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// drawRectOutline draws an outline rectangle
func drawRectOutline(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	// Top and bottom
	for dx := 0; dx < w; dx++ {
		img.Set(x+dx, y, col)
		img.Set(x+dx, y+h-1, col)
	}
	// Left and right
	for dy := 0; dy < h; dy++ {
		img.Set(x, y+dy, col)
		img.Set(x+w-1, y+dy, col)
	}
}

// drawRect draws a simple rectangle
func drawRect(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			px := x + dx
			py := y + dy
			if px >= 0 && px < img.Bounds().Dx() && py >= 0 && py < img.Bounds().Dy() {
				img.Set(px, py, col)
			}
		}
	}
}
func keyTextBg(text string, bg color.RGBA) *image.RGBA {
	img := newImg()
	fillRect(img, img.Bounds(), bg)
	drawText(img, (W-measureText(text, 14))/2, (H-14)/2, text, color.RGBA{255, 255, 255, 255}, 14)
	return img
}

// --- Fetch ---
func fetchProf(u string) image.Image {
	if v, ok := profCache.Get(u); ok {
		return v.(image.Image)
	}
	p := filepath.Join(profDir, hsh(u)+".jpg")
	if f, err := os.Open(p); err == nil {
		defer f.Close()
		if img, err := jpeg.Decode(f); err == nil {
			resized := resize72(img)
			profCache.Set(u, resized)
			return resized
		}
	}
	resp, err := httpClient.Get(u)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	os.WriteFile(p, data, 0644)
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		img, err = png.Decode(bytes.NewReader(data))
	}
	if err == nil {
		resized := resize72(img)
		profCache.Set(u, resized)
		return resized
	}
	return nil
}

func twitchGet(u string, params url.Values) map[string]interface{} {
	// Check if environment variables are set
	if CID == "" || AT == "" {
		debugLog("Environment variables not set, skipping API call: %s", u)
		showTokenError("Missing Client ID or Access Token")
		return map[string]interface{}{}
	}

	if params != nil {
		u += "?" + params.Encode()
	}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Client-ID", CID)
	req.Header.Set("Authorization", "Bearer "+AT)
	resp, err := httpClient.Do(req)

	if err != nil {
		log.Printf("[API ERROR] Network error URL: %s, reason: %v", u, err)
		if logAnalyzer != nil {
			logAnalyzer.LogAPIError(u, 0, fmt.Sprintf("Network error: %v", err))
		}
		return map[string]interface{}{}
	}
	defer resp.Body.Close()

	// Handle token errors
	if resp.StatusCode == 401 {
		warnLog("Token expired or invalid (HTTP 401)")
		if logAnalyzer != nil {
			logAnalyzer.LogAPIError(u, 401, "Token expired or invalid")
		}

		// Try to refresh token if refresh token is available
		if RT != "" && CS != "" {
			infoLog("Attempting token refresh...")
			refReq, _ := http.NewRequest("POST", "https://id.twitch.tv/oauth2/token", strings.NewReader(url.Values{
				"grant_type":    {"refresh_token"},
				"refresh_token": {RT},
				"client_id":     {CID},
				"client_secret": {CS},
			}.Encode()))
			refReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			refResp, err := httpClient.Do(refReq)
			if err != nil {
				log.Printf("[API ERROR] Refresh request failed: %v", err)
				showTokenError("Token refresh failed. Please re-authenticate.")
			} else {
				defer refResp.Body.Close()
				var refRes map[string]interface{}
				json.NewDecoder(refResp.Body).Decode(&refRes)

				if newToken, ok := refRes["access_token"].(string); ok {
					AT = newToken
					log.Println("[API INFO] Token refresh successful!")

					// Update token in token manager
					if tokenManager != nil && tokenManager.GetCurrentToken() != nil {
						token := tokenManager.GetCurrentToken()
						token.AccessToken = AT
						if newRefresh, ok := refRes["refresh_token"].(string); ok {
							token.RefreshToken = newRefresh
							RT = newRefresh
						}
						token.ExpiresAt = time.Now().Add(24 * time.Hour)
						tokenManager.SaveToken(token)
					}

					// Retry the original request
					req.Header.Set("Authorization", "Bearer "+AT)
					retryResp, err := httpClient.Do(req)
					if err != nil {
						log.Printf("[API ERROR] Retry after refresh failed: %v", err)
						return map[string]interface{}{}
					}
					defer retryResp.Body.Close()
					if retryResp.StatusCode >= 400 {
						showTokenError("API request failed after token refresh")
						return map[string]interface{}{}
					}
					var retryRes map[string]interface{}
					json.NewDecoder(retryResp.Body).Decode(&retryRes)
					return retryRes
				}
			}
		}
		// If refresh failed or not available, show error
		showTokenError("Access token invalid or expired. Please re-authenticate.")
		return map[string]interface{}{}
	} else if resp.StatusCode >= 400 {
		log.Printf("[API ERROR] Request failed with status: %d", resp.StatusCode)
		if logAnalyzer != nil {
			logAnalyzer.LogAPIError(u, resp.StatusCode, "API request failed")
		}
		return map[string]interface{}{}
	}

	var res map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&res)
	if res == nil {
		res = map[string]interface{}{}
	}
	return res
}

func fetchFollowedFromAPI() []string {
	if CID == "" || AT == "" || UID == "" {
		log.Println("[API INFO] 環境変数が不足しているためフォローリストの取得をスキップ")
		return nil
	}
	var out []string
	cursor := ""
	for {
		params := url.Values{"user_id": {UID}, "first": {"100"}}
		if cursor != "" {
			params.Set("after", cursor)
		}
		js := twitchGet("https://api.twitch.tv/helix/channels/followed", params)
		data, ok := js["data"].([]interface{})
		if !ok || len(data) == 0 {
			break
		}
		for _, item := range data {
			m, _ := item.(map[string]interface{})
			lg := strings.ToLower(fmt.Sprintf("%v", m["broadcaster_login"]))
			if lg != "" {
				out = append(out, lg)
			}
		}
		pag, _ := js["pagination"].(map[string]interface{})
		cursor, _ = pag["cursor"].(string)
		if cursor == "" {
			break
		}
	}
	return out
}

func fetchUsers(logins []string) {
	for i := 0; i < len(logins); i += 100 {
		end := min(i+100, len(logins))
		params := url.Values{}
		for _, l := range logins[i:end] {
			params.Add("login", l)
		}
		js := twitchGet("https://api.twitch.tv/helix/users", params)
		if data, ok := js["data"].([]interface{}); ok {
			for _, item := range data {
				u := item.(map[string]interface{})
				lg := strings.ToLower(fmt.Sprintf("%v", u["login"]))
				stateMu.Lock()
				lu[lg] = u
				id2lg[fmt.Sprintf("%v", u["id"])] = lg
				stateMu.Unlock()
			}
		}
	}
}

func fetchStreams() {
	if CID == "" || AT == "" {
		log.Println("[API INFO] 環境変数が設定されていないため配信情報の取得をスキップ")
		return
	}
	if len(followed) == 0 {
		return
	}
	stateMu.RLock()
	var uids []string
	for _, f := range followed {
		if u, ok := lu[f]; ok {
			uids = append(uids, fmt.Sprintf("%v", u["id"]))
		}
	}
	stateMu.RUnlock()

	var online []map[string]interface{}
	hasError := false

	for i := 0; i < len(uids); i += 100 {
		end := min(i+100, len(uids))
		params := url.Values{}
		for _, uid := range uids[i:end] {
			params.Add("user_id", uid)
		}
		js := twitchGet("https://api.twitch.tv/helix/streams", params)

		if data, ok := js["data"].([]interface{}); ok {
			for _, item := range data {
				online = append(online, item.(map[string]interface{}))
			}
		} else {
			hasError = true
			break
		}
	}

	if hasError {
		return
	}

	for i := 0; i < len(online); i++ {
		for j := i + 1; j < len(online); j++ {
			if online[j]["viewer_count"].(float64) > online[i]["viewer_count"].(float64) {
				online[i], online[j] = online[j], online[i]
			}
		}
	}
	if len(online) > MAX_TWITCH_KEYS {
		online = online[:MAX_TWITCH_KEYS]
	}

	stateMu.Lock()
	twOrder = nil
	views = map[string]int{}
	startedAt = map[string]float64{}
	for _, s := range online {
		lg := id2lg[fmt.Sprintf("%v", s["user_id"])]
		if lg == "" {
			continue
		}
		twOrder = append(twOrder, lg)
		views[lg] = int(s["viewer_count"].(float64))
		title := fmt.Sprintf("%v", s["title"])
		titles[lg] = title
		titleStep[lg] = (float64(measureText(title+"   ", 14)) / math.Max(2.0, minF(8.0, float64(len([]rune(title)))*0.2+1.5))) * SCROLL_IV
		titleW[lg] = float64(measureText(title+"   ", 14))
		game := fmt.Sprintf("%v", s["game_name"])
		categories[lg] = game
		catStep[lg] = (float64(measureText(game+"   ", 14)) / math.Max(2.0, minF(8.0, float64(len([]rune(game)))*0.2+1.5))) * SCROLL_IV
		catW[lg] = float64(measureText(game+"   ", 14))
		if t, err := time.Parse(time.RFC3339, fmt.Sprintf("%v", s["started_at"])); err == nil {
			startedAt[lg] = float64(t.Unix())
		}
	}
	stateMu.Unlock()

	currentOnline := len(online)
	if currentOnline != lastOnlineCount {
		if currentOnline == 0 {
			log.Println("[API INFO] 現在配信中のフォローユーザーはいません (0人オンライン)")
		} else {
			log.Printf("[API INFO] 現在 %d 人が配信中です\n", currentOnline)
		}
		lastOnlineCount = currentOnline
	}
}
func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// --- Renderers ---
func renderHome() {
	deckMu.Lock()
	sdeck.FillImage(0, keyTextBg("Twitch", color.RGBA{100, 0, 255, 255}))
	sdeck.FillImage(1, keyTextBg("OAuth", color.RGBA{0, 100, 200, 255}))
	sdeck.FillImage(2, keyTextBg("Setting", color.RGBA{40, 40, 40, 255}))
	for i := 3; i < MAX_KEYS; i++ {
		sdeck.FillBlank(i)
	}
	deckMu.Unlock()
}

func twImg(profURL, login string) *image.RGBA {
	img := newImg()
	prof := fetchProf(profURL)
	if prof != nil {
		draw.Draw(img, img.Bounds(), prof, image.Point{}, draw.Src)
	} else {
		fillRect(img, img.Bounds(), color.RGBA{30, 30, 30, 255})
		drawText(img, 4, 25, login, color.RGBA{255, 255, 255, 255}, 12)
	}

	stateMu.RLock()
	v, st := views[login], startedAt[login]
	txt, ofs := titles[login], titleOfs[login]
	if scrollMode == "category" {
		txt, ofs = categories[login], catOfs[login]
	}
	stateMu.RUnlock()

	if v > 0 {
		s := formatViewerCount(v)
		tw := measureText(s, 11)
		fillRect(img, image.Rect(4, 3, tw+10, 18), color.RGBA{40, 80, 120, 220}) // 青色に調整
		drawText(img, 7, 4, s, color.RGBA{255, 255, 255, 255}, 11)
	}
	if st > 0 {
		el := time.Now().Unix() - int64(st)
		h, m := el/3600, (el%3600)/60
		lab := fmt.Sprintf("%dm", m)
		if h > 0 {
			lab = fmt.Sprintf("%dh%dm", h, m)
		}
		tw := measureText(lab, 11)
		xOffset := W - tw - 15
		fillRect(img, image.Rect(xOffset, 3, xOffset+tw+6, 18), color.RGBA{120, 60, 40, 220}) // オレンジ色に調整
		drawText(img, xOffset+3, 4, lab, color.RGBA{255, 255, 255, 255}, 11)
	}
	if txt != "" {
		col := color.RGBA{255, 217, 0, 255}
		if scrollMode == "category" {
			col = color.RGBA{200, 245, 255, 255}
		}
		y := H - 18 // 2ピクセル上に調整
		fillRect(img, image.Rect(0, y-2, W, H), color.RGBA{0, 0, 0, 230})
		tx := txt + "   "
		if tw := float64(measureText(tx, 14)); tw > 0 {
			xp := -int(math.Mod(ofs, tw))
			drawText(img, xp, y, tx, col, 14)
			if float64(xp)+tw < float64(W) {
				drawText(img, xp+int(tw), y, tx, col, 14)
			}
		}
	}
	return img
}

func renderTW() {
	stateMu.RLock()
	order := append([]string{}, twOrder...)
	stateMu.RUnlock()
	deckMu.Lock()
	for i := 0; i < MAX_TWITCH_KEYS; i++ {
		if i < len(order) {
			lg := order[i]
			u := lu[lg]
			if u != nil {
				sdeck.FillImage(i, twImg(fmt.Sprintf("%v", u["profile_image_url"]), lg))
			} else {
				sdeck.FillImage(i, keyTextBg(lg, color.RGBA{0, 0, 0, 255}))
			}
		} else {
			sdeck.FillBlank(i)
		}
	}
	sdeck.FillImage(14, keyTextBg("ホーム", color.RGBA{50, 0, 50, 255}))
	deckMu.Unlock()
}

func renderLV(lg string) {
	deckMu.Lock()
	for i := 0; i < 15; i++ {
		sdeck.FillBlank(i)
	}
	for i := 0; i < len(EMOTES); i++ {
		sdeck.FillImage(i, keyTextBg(EMOTES[i], color.RGBA{0, 0, 0, 255}))
	}
	sdeck.FillImage(11, keyTextBg("配信を見る", color.RGBA{20, 40, 20, 255}))
	sdeck.FillImage(12, keyTextBg("TEXT", color.RGBA{20, 20, 40, 255}))
	sdeck.FillImage(13, keyTextBg("ホーム", color.RGBA{0, 40, 40, 255}))
	sdeck.FillImage(14, keyTextBg("戻る", color.RGBA{40, 40, 0, 255}))
	deckMu.Unlock()
}

func renderTX() {
	deckMu.Lock()
	for i := 0; i < 15; i++ {
		sdeck.FillBlank(i)
	}
	for i := 0; i < len(DEFAULT_TEXTS); i++ {
		sdeck.FillImage(i, keyTextBg(DEFAULT_TEXTS[i], color.RGBA{30, 30, 30, 255}))
	}
	sdeck.FillImage(12, keyTextBg("NEXT", color.RGBA{30, 0, 30, 255}))
	sdeck.FillImage(13, keyTextBg("ホーム", color.RGBA{0, 40, 40, 255}))
	sdeck.FillImage(14, keyTextBg("戻る", color.RGBA{40, 40, 0, 255}))
	deckMu.Unlock()
}

func renderNX() {
	deckMu.Lock()
	for i := 0; i < 15; i++ {
		sdeck.FillBlank(i)
	}
	for i := 0; i < len(DEFAULT_NEXT); i++ {
		sdeck.FillImage(i, keyTextBg(DEFAULT_NEXT[i], color.RGBA{30, 30, 30, 255}))
	}
	sdeck.FillImage(13, keyTextBg("ホーム", color.RGBA{0, 40, 40, 255}))
	sdeck.FillImage(14, keyTextBg("戻る", color.RGBA{40, 40, 0, 255}))
	deckMu.Unlock()
}

func renderST() {
	deckMu.Lock()
	for i := 0; i < 15; i++ {
		sdeck.FillBlank(i)
	}
	sdeck.FillImage(0, keyTextBg("StreamDeck", color.RGBA{30, 30, 30, 255}))
	sdeck.FillImage(1, keyTextBg("再起動", color.RGBA{60, 0, 0, 255}))
	// ボタン2-13は空白（仕様書通り[0]）
	sdeck.FillImage(14, keyTextBg("ホーム", color.RGBA{0, 40, 40, 255}))
	deckMu.Unlock()
}

func renderSD() {
	deckMu.Lock()
	for i := 0; i < 15; i++ {
		sdeck.FillBlank(i)
	}
	sdeck.FillImage(0, keyTextBg("明るさUP", color.RGBA{40, 40, 0, 255}))
	sdeck.FillImage(1, keyTextBg("明るさDW", color.RGBA{40, 0, 0, 255}))
	sdeck.FillImage(13, keyTextBg("ホーム", color.RGBA{0, 40, 40, 255}))
	sdeck.FillImage(14, keyTextBg("戻る", color.RGBA{40, 40, 0, 255}))
	deckMu.Unlock()
}

func show(pg, ctx string, st bool) {
	if st && page != pg {
		stack = append(stack, stackEntry{page, live})
	}
	page, live = pg, ctx
	deckMu.Lock()
	for i := 0; i < MAX_KEYS; i++ {
		sdeck.prevImages[i] = ""
	}
	deckMu.Unlock()
	switch pg {
	case HOME:
		renderHome()
	case TW:
		renderTW()
	case LV:
		renderLV(ctx)
	case TX:
		renderTX()
	case NX:
		renderNX()
	case ST:
		renderST()
	case SD:
		renderSD()
	case OA:
		renderOA()
	}
}
func back() {
	if len(stack) == 0 {
		show(HOME, "", false)
		return
	}
	e := stack[len(stack)-1]
	stack = stack[:len(stack)-1]
	show(e.page, e.ctx, false)
}

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
			// 認証ボタン
			startOAuth()
		} else if k == 1 {
			// 取得ボタン
			getTokenFromClipboard()
		} else if k == 2 {
			// env保存ボタン
			saveEnvVars()
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
		case 1:
			return "Get"
		case 2:
			return "Save env"
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
)

// fetchIRCUsername fetches the Twitch username from the API
// Returns true if successful, false otherwise
func fetchIRCUsername() bool {
	ircUsernameTries++

	if AT == "" {
		log.Println("[IRC] ユーザー名取得失敗: アクセストークンがありません")
		return false
	}

	log.Printf("[IRC] APIからユーザー名を取得中... (試行 %d)", ircUsernameTries)

	// スコープチェック（ユーザー情報取得に必要なスコープ）
	requiredScopes := []string{"user:read:email", "user:read"}
	hasScope := false
	for _, scope := range requiredScopes {
		if strings.Contains(SCOPE, scope) {
			hasScope = true
			break
		}
	}

	if !hasScope {
		log.Printf("[IRC WARN] スコープ不足の可能性: 現在のスコープ: %s", SCOPE)
		log.Printf("[IRC WARN] ユーザー情報取得には以下のいずれかが必要: %v", requiredScopes)
		// 続行（既存のトークンで試す）
	}

	req, err := http.NewRequest("GET", "https://api.twitch.tv/helix/users", nil)
	if err != nil {
		log.Printf("[IRC] ユーザー名取得リクエスト作成失敗: %v", err)
		return false
	}

	req.Header.Set("Client-ID", CID)
	req.Header.Set("Authorization", "Bearer "+AT)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[IRC] ユーザー名取得失敗: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("[IRC] ユーザー名取得エラー: %s", resp.Status)

		// トークンが無効な場合
		if resp.StatusCode == 401 {
			log.Println("[IRC] アクセストークンが無効です。再認証が必要です。")
			showTokenError("アクセストークンが無効です。OAuthボタンで再認証してください。")
		}
		return false
	}

	var result struct {
		Data []struct {
			ID              string `json:"id"`
			Login           string `json:"login"`
			DisplayName     string `json:"display_name"`
			BroadcasterType string `json:"broadcaster_type"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[IRC] ユーザー名レスポンス解析失敗: %v", err)
		return false
	}

	if len(result.Data) == 0 {
		log.Println("[IRC] ユーザー名取得失敗: ユーザーデータがありません")
		return false
	}

	ircMu.Lock()
	ircUsername = strings.ToLower(result.Data[0].Login)
	ircUsernameTries = 0 // 成功したらリセット
	ircMu.Unlock()

	// 環境変数とグローバル変数を更新
	os.Setenv("TWITCH_USER_ID", ircUsername)
	UID = ircUsername

	// 設定ファイルにも保存
	saveUsernameToConfig(ircUsername)

	log.Printf("[IRC] ユーザー名取得成功: %s (ID: %s)", ircUsername, result.Data[0].ID)
	return true
}

// saveUsernameToConfig saves the username to config file
func saveUsernameToConfig(username string) {
	configPath := filepath.Join(os.Getenv("HOME"), ".config", "streamdeck-twitch", "config.json")

	// 既存の設定を読み込み
	configData := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		json.Unmarshal(data, &configData)
	}

	// ユーザー名を追加/更新
	configData["username"] = username

	// 保存
	if data, err := json.MarshalIndent(configData, "", "  "); err == nil {
		os.WriteFile(configPath, data, 0600)
		log.Printf("[CONFIG] ユーザー名を設定ファイルに保存: %s", username)
	}
}

// loadUsernameFromConfig loads username from config file
func loadUsernameFromConfig() string {
	configPath := filepath.Join(os.Getenv("HOME"), ".config", "streamdeck-twitch", "config.json")

	if data, err := os.ReadFile(configPath); err == nil {
		var configData map[string]interface{}
		if err := json.Unmarshal(data, &configData); err == nil {
			if username, ok := configData["username"].(string); ok {
				return strings.ToLower(username)
			}
		}
	}
	return ""
}

func ircLoop() {
	for {
		// アクセストークンがない場合は接続を試みない
		if AT == "" {
			time.Sleep(5 * time.Second)
			continue
		}

		// ユーザー名を確実に取得（初回のみ）
		if ircUsername == "" {
			usernameFromConfig := loadUsernameFromConfig()

			if usernameFromConfig != "" {
				// 1. 設定ファイルから読み込み
				ircUsername = usernameFromConfig
				log.Printf("[IRC] 設定ファイルからユーザー名読み込み: %s", ircUsername)
			} else if UID != "" {
				// 2. 環境変数から
				ircUsername = strings.ToLower(UID)
				log.Printf("[IRC] 環境変数からユーザー名設定: %s", ircUsername)
			} else if AT != "" {
				// 3. APIから取得（同期的に）
				log.Println("[IRC] APIからユーザー名を取得します...")
				if fetchIRCUsername() {
					log.Printf("[IRC] APIからユーザー名取得成功: %s", ircUsername)
				} else {
					// API取得失敗時は匿名ユーザーを使用（読み取り専用）
					ircUsername = "justinfan12345"
					log.Printf("[IRC] API取得失敗、匿名ユーザーを使用: %s (送信不可)", ircUsername)
				}
			} else {
				// 4. デフォルト（最終手段）
				ircUsername = "justinfan12345"
				log.Printf("[IRC] デフォルト匿名ユーザーを使用: %s (送信不可)", ircUsername)
			}
		}

		ircMu.Lock()
		if ircConn == nil {
			log.Printf("[IRC DEBUG] IRC接続試行: token=%v, username=%v", AT != "", ircUsername)

			// アクセストークンがない場合は接続しない
			if AT == "" {
				log.Println("[IRC DEBUG] アクセストークンなし、接続スキップ")
				ircMu.Unlock()
				time.Sleep(5 * time.Second)
				continue
			}

			// ユーザー名が未設定の場合は取得を試みる
			if ircUsername == "" || strings.HasPrefix(ircUsername, "justinfan") {
				log.Println("[IRC DEBUG] 有効なユーザー名がありません。取得を試みます...")
				ircMu.Unlock()

				// ユーザー名取得を試みる
				if AT != "" {
					fetchIRCUsername()
				}

				time.Sleep(2 * time.Second)
				continue
			}

			if c, err := net.Dial("tcp", "irc.chat.twitch.tv:6667"); err == nil {
				log.Println("[IRC] Twitch IRCに接続しました")

				// Twitch IRC接続シーケンス
				tokenPreview := "none"
				if len(AT) > 10 {
					tokenPreview = AT[:10] + "..."
				} else if AT != "" {
					tokenPreview = "present"
				}
				log.Printf("[IRC DEBUG] 認証送信: PASS oauth:%s", tokenPreview)
				fmt.Fprintf(c, "PASS oauth:%s\r\n", AT)

				// ニックネーム設定
				nick := ircUsername
				// justinfan系ユーザーの場合は読み取り専用モード
				isReadOnly := strings.HasPrefix(nick, "justinfan")
				if isReadOnly {
					log.Printf("[IRC DEBUG] 読み取り専用モード: NICK %s", nick)
				} else {
					log.Printf("[IRC DEBUG] 送信可能モード: NICK %s", nick)
				}
				fmt.Fprintf(c, "NICK %s\r\n", nick)

				log.Println("[IRC DEBUG] CAPABILITY要求送信")
				fmt.Fprintf(c, "CAP REQ :twitch.tv/tags twitch.tv/commands twitch.tv/membership\r\n")

				ircConn = c
				ircJoined = make(map[string]bool) // 参加済みチャンネルをリセット
				log.Println("[IRC DEBUG] IRC接続初期化完了")
			} else {
				log.Printf("[IRC] 接続失敗: %v", err)
				ircMu.Unlock()
				time.Sleep(5 * time.Second)
				continue
			}
		}

		ircMu.Lock()
		if ircConn == nil {
			log.Printf("[IRC DEBUG] IRC接続試行: token=%v, username=%v", AT != "", ircUsername)
			if c, err := net.Dial("tcp", "irc.chat.twitch.tv:6667"); err == nil {
				log.Println("[IRC] Twitch IRCに接続しました")

				// Twitch IRC接続シーケンス
				log.Printf("[IRC DEBUG] 認証送信: PASS oauth:%s", AT[:min(10, len(AT))]+"...")
				fmt.Fprintf(c, "PASS oauth:%s\r\n", AT)

				nick := "justinfan12345"
				if ircUsername != "" {
					nick = ircUsername
				}
				log.Printf("[IRC DEBUG] ニックネーム設定: NICK %s", nick)
				fmt.Fprintf(c, "NICK %s\r\n", nick)

				log.Println("[IRC DEBUG] CAPABILITY要求送信")
				fmt.Fprintf(c, "CAP REQ :twitch.tv/tags twitch.tv/commands twitch.tv/membership\r\n")

				ircConn = c
				ircJoined = make(map[string]bool) // 参加済みチャンネルをリセット
				log.Println("[IRC DEBUG] IRC接続初期化完了")
			} else {
				log.Printf("[IRC] 接続失敗: %v", err)
				ircMu.Unlock()
				time.Sleep(5 * time.Second)
				continue
			}
		}

		// 現在のライブチャンネルに参加
		if live != "" && !ircJoined[live] {
			log.Printf("[IRC] チャンネルに参加: #%s", live)
			fmt.Fprintf(ircConn, "JOIN #%s\r\n", live)
			ircJoined[live] = true
			log.Printf("[IRC DEBUG] Joined channel: #%s (joined map: %v)", live, ircJoined)
		}

		// メッセージ受信
		ircConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		buf := make([]byte, 4096)
		n, err := ircConn.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				ircMu.Unlock()
				continue
			}
			log.Printf("[IRC] 接続エラー: %v", err)
			ircConn.Close()
			ircConn = nil
			ircMu.Unlock()
			time.Sleep(5 * time.Second)
			continue
		}

		// 受信データ処理
		data := string(buf[:n])
		lines := strings.Split(data, "\r\n")
		for _, line := range lines {
			if line == "" {
				continue
			}

			// PING応答
			if strings.HasPrefix(line, "PING") {
				fmt.Fprintf(ircConn, "PONG :tmi.twitch.tv\r\n")
				log.Println("[IRC] PINGに応答")
			}

			// 接続確認メッセージ
			if strings.Contains(line, "Welcome, GLHF!") {
				log.Println("[IRC] Twitch IRCに正常に接続されました")
			}
		}
		ircMu.Unlock()
	}
}

func ircSend(ch, msg string) {
	log.Printf("[CHAT] チャット送信試行: #%s -> %s", ch, msg)
	log.Printf("[CHAT DEBUG] 現在の状態: AT=%v, CID=%v, ircUsername=%s", AT != "", CID != "", ircUsername)

	// Twitch APIを使用したチャット送信
	sendChatMessage(ch, msg)
}

// sendChatMessage sends a chat message using Twitch Helix API
func sendChatMessage(channel, message string) {
	log.Printf("[CHAT DEBUG] sendChatMessage called: channel=%s, message=%s", channel, message)

	if AT == "" || CID == "" {
		log.Printf("[CHAT ERROR] 送信失敗: アクセストークンまたはClient IDがありません")
		log.Printf("[CHAT DEBUG] AT empty: %v, CID empty: %v", AT == "", CID == "")
		return
	}

	if channel == "" {
		log.Printf("[CHAT ERROR] 送信失敗: チャンネル名が空です")
		return
	}

	if message == "" {
		log.Printf("[CHAT ERROR] 送信失敗: メッセージが空です")
		return
	}

	// チャンネルIDを取得（ユーザー名から）
	channelID, err := getChannelID(channel)
	if err != nil {
		log.Printf("[CHAT ERROR] チャンネルID取得失敗: %v", err)
		return
	}

	// デバッグログ
	log.Printf("[CHAT DEBUG] チャンネルID: %s (for %s)", channelID, channel)

	// ブロードキャスターIDを取得（送信者）
	broadcasterID, err := getBroadcasterID()
	if err != nil {
		log.Printf("[CHAT WARN] ブロードキャスターID取得失敗: %v", err)
		log.Printf("[CHAT INFO] broadcasterIDなしでIRC送信を試みます")
		broadcasterID = "" // 空でも続行
	}

	// Twitch Helix API: POST /helix/chat/messages
	// 注意: このエンドポイントは現在ベータ版で、特別なアクセス権が必要かもしれません
	// 代替として、従来のIRCを使用するか、別の方法を検討

	log.Printf("[CHAT WARN] Twitch Helix chat/messages APIは制限がある可能性があります")
	log.Printf("[CHAT INFO] 代替方法としてIRC送信を試みます")

	// IRCを使用した送信（OAuthトークンとユーザー名が必要）
	log.Printf("[CHAT DEBUG] broadcasterID取得結果: %s", broadcasterID)

	// broadcasterIDが空でもIRC送信を試みる
	sendViaIRC(channel, message, broadcasterID)
}

// getChannelID gets the channel ID from username
func getChannelID(username string) (string, error) {
	if username == "" {
		return "", fmt.Errorf("ユーザー名が空です")
	}

	// キャッシュがあれば使用
	if cachedID, ok := id2lg[username]; ok {
		return cachedID, nil
	}

	// APIから取得
	url := fmt.Sprintf("https://api.twitch.tv/helix/users?login=%s", username)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Client-ID", CID)
	req.Header.Set("Authorization", "Bearer "+AT)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("APIエラー: %s", resp.Status)
	}

	var result struct {
		Data []struct {
			ID    string `json:"id"`
			Login string `json:"login"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if len(result.Data) == 0 {
		return "", fmt.Errorf("ユーザーが見つかりません: %s", username)
	}

	// キャッシュに保存
	id2lg[username] = result.Data[0].ID
	return result.Data[0].ID, nil
}

// getBroadcasterID gets the broadcaster ID (current user)
func getBroadcasterID() (string, error) {
	if UID != "" {
		return UID, nil
	}

	// APIから取得
	url := "https://api.twitch.tv/helix/users"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Client-ID", CID)
	req.Header.Set("Authorization", "Bearer "+AT)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("APIエラー: %s", resp.Status)
	}

	var result struct {
		Data []struct {
			ID    string `json:"id"`
			Login string `json:"login"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if len(result.Data) == 0 {
		return "", fmt.Errorf("ユーザー情報が取得できません")
	}

	// 環境変数とグローバル変数を更新
	UID = result.Data[0].ID
	os.Setenv("TWITCH_USER_ID", UID)

	return UID, nil
}

// sendViaIRC sends a message via IRC with proper OAuth authentication
func sendViaIRC(channel, message, broadcasterID string) {
	log.Printf("[IRC SEND DEBUG] sendViaIRC called: channel=%s, message=%s", channel, message)

	ircMu.Lock()
	defer ircMu.Unlock()

	log.Printf("[IRC SEND] 送信試行: #%s -> %s", channel, message)

	// 必須チェック
	log.Printf("[IRC SEND DEBUG] 必須チェック: AT=%v, ircConn=%v, ircUsername=%s", AT != "", ircConn != nil, ircUsername)
	if AT == "" {
		log.Printf("[IRC SEND ERROR] アクセストークンがありません")
		return
	}

	// スコープチェック（チャット送信に必要なスコープ）
	log.Printf("[IRC SEND DEBUG] スコープチェック: 現在のスコープ=%s", SCOPE)
	requiredChatScopes := []string{"user:write:chat", "chat:edit"}
	hasChatScope := false
	for _, scope := range requiredChatScopes {
		if strings.Contains(SCOPE, scope) {
			hasChatScope = true
			log.Printf("[IRC SEND DEBUG] 必要なスコープを確認: %s", scope)
			break
		}
	}

	if !hasChatScope {
		log.Printf("[IRC SEND ERROR] スコープ不足: チャット送信には以下のいずれかが必要: %v", requiredChatScopes)
		log.Printf("[IRC SEND ERROR] 現在のスコープ: %s", SCOPE)
		log.Printf("[IRC SEND INFO] OAuth認証をやり直して適切なスコープを取得してください")
		return
	}

	log.Printf("[IRC SEND DEBUG] スコープチェック通過")

	if ircConn == nil {
		log.Printf("[IRC SEND ERROR] IRC接続がありません")
		return
	}

	// ユーザー名がjustinfan系でないことを確認
	log.Printf("[IRC SEND DEBUG] ユーザー名チェック: ircUsername=%s", ircUsername)
	if strings.HasPrefix(ircUsername, "justinfan") {
		log.Printf("[IRC SEND ERROR] 匿名ユーザー %s では送信できません", ircUsername)
		log.Printf("[IRC SEND INFO] OAuth認証を行って有効なユーザー名を取得してください")

		// ユーザー名を再取得してみる
		log.Println("[IRC SEND] ユーザー名を再取得します...")
		if fetchIRCUsername() {
			log.Printf("[IRC SEND] ユーザー名再取得成功: %s", ircUsername)
			// 再取得後もjustinfan系なら送信不可
			if strings.HasPrefix(ircUsername, "justinfan") {
				return
			}
		} else {
			return
		}
	}

	log.Printf("[IRC SEND DEBUG] ユーザー名チェック通過")

	// チャンネルに参加しているか確認・参加
	if !ircJoined[channel] {
		log.Printf("[IRC SEND] チャンネルに参加: #%s", channel)
		joinCmd := fmt.Sprintf("JOIN #%s\r\n", channel)
		if _, err := fmt.Fprintf(ircConn, joinCmd); err != nil {
			log.Printf("[IRC SEND ERROR] チャンネル参加失敗: %v", err)
			return
		}
		ircJoined[channel] = true
		time.Sleep(200 * time.Millisecond) // 参加処理待ち
	}

	// メッセージ送信
	msgCmd := fmt.Sprintf("PRIVMSG #%s :%s\r\n", channel, message)
	log.Printf("[IRC SEND DEBUG] 送信コマンド: %s", strings.TrimSpace(msgCmd))

	n, err := fmt.Fprintf(ircConn, msgCmd)
	if err != nil {
		log.Printf("[IRC SEND ERROR] 送信失敗: %v (bytes: %d)", err, n)

		// 接続エラーの場合は再接続
		if strings.Contains(err.Error(), "broken pipe") ||
			strings.Contains(err.Error(), "connection reset") {
			log.Println("[IRC SEND] 接続エラー、再接続を試みます")
			if ircConn != nil {
				ircConn.Close()
			}
			ircConn = nil
		}
	} else {
		log.Printf("[IRC SEND SUCCESS] 送信完了: #%s -> %s (%d bytes)", channel, message, n)
	}
}

// --- Loops ---
func bgLoop() {
	for {
		fetchStreams()
		time.Sleep(FETCH_IV * time.Second)
	}
}
func mainLoop() {
	lastFetch, lastScroll := time.Now(), time.Now()
	for {
		now := time.Now()
		if now.Sub(lastFetch).Seconds() > FETCH_IV {
			lastFetch = now
			if page == TW {
				renderTW()
			}
		}
		// コメント窓以外で1分以上の無操作状態の場合TWITCH窓へ移行
		if page != TW && page != LV && page != TX && page != NX && now.Sub(lastInput).Seconds() > IDLE_TIMEOUT {
			show(TW, "", false)
		}
		if now.Sub(lastScroll).Seconds() > SCROLL_IV {
			lastScroll = now
			stateMu.Lock()
			if scrollMode == "title" {
				allDone := true
				for _, lg := range twOrder {
					titleOfs[lg] += titleStep[lg]
					if titleW[lg] > 0 && math.Mod(titleOfs[lg], titleW[lg])+titleStep[lg] >= titleW[lg] {
						titleWrapped[lg] = true
					}
					if !titleWrapped[lg] {
						allDone = false
					}
				}
				if allDone && len(twOrder) > 0 {
					scrollMode = "category"
					for _, lg := range twOrder {
						catOfs[lg] = 0
						catWrapped[lg] = false
					}
				}
			} else {
				allDone := true
				for _, lg := range twOrder {
					catOfs[lg] += catStep[lg]
					if catW[lg] > 0 && math.Mod(catOfs[lg], catW[lg])+catStep[lg] >= catW[lg] {
						catWrapped[lg] = true
					}
					if !catWrapped[lg] {
						allDone = false
					}
				}
				if allDone && len(twOrder) > 0 {
					scrollMode = "title"
					for _, lg := range twOrder {
						titleOfs[lg] = 0
						titleWrapped[lg] = false
					}
				}
			}
			stateMu.Unlock()
			if page == TW {
				renderTW()
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func renderOA() {
	deckMu.Lock()
	for i := 0; i < 15; i++ {
		sdeck.FillBlank(i)
	}
	sdeck.FillImage(0, keyTextBg("Auth", color.RGBA{0, 100, 200, 255}))
	sdeck.FillImage(1, keyTextBg("Get", color.RGBA{100, 0, 200, 255}))
	sdeck.FillImage(2, keyTextBg("Save env", color.RGBA{200, 100, 0, 255}))
	sdeck.FillImage(14, keyTextBg("Back", color.RGBA{40, 40, 0, 255}))
	deckMu.Unlock()
}

// --- Follow Cache ---
func loadFollowedFromCache() []string {
	data, err := os.ReadFile(followCachePath)
	if err != nil {
		return nil
	}

	var followed []string
	if err := json.Unmarshal(data, &followed); err != nil {
		return nil
	}

	log.Printf("[Cache] フォローリストをキャッシュから読み込みました (%d人)", len(followed))
	return followed
}

func saveFollowedToCache(followed []string) {
	data, err := json.Marshal(followed)
	if err != nil {
		log.Printf("[Cache] フォローリストのキャッシュ保存エラー: %v", err)
		return
	}

	if err := os.WriteFile(followCachePath, data, 0644); err != nil {
		log.Printf("[Cache] フォローリストのキャッシュ保存エラー: %v", err)
		return
	}

	log.Printf("[Cache] フォローリストをキャッシュに保存しました (%d人)", len(followed))
}
