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
	"sync"
	"time"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font/sfnt"
)

const (
	V2_PAGE_PACKET_SZ = 1024
	V2_ITER_SZ        = 1016
	V2_HEADER_SZ      = 8
	MAX_KEYS          = 15
	MAX_TWITCH_KEYS   = 14
	SCROLL_IV         = 0.033
	FETCH_IV          = 3
	IDLE_TIMEOUT      = 60.0
	W                 = 72
	H                 = 72
)

const (
	HOME = "home"
	TW   = "tw"
	LV   = "lv"
	TX   = "tx"
	NX   = "nx"
	ST   = "st"
	SD   = "sd"
	OA   = "oa"
	FN   = "fn"
	UI   = "ui"
)

var (
	CID, CS, SCOPE, AT, RT, UID string

	EMOTES     = []string{"BloodTrail", "HeyGuys", "LUL", "DinoDance", "HungryPaimon", "GlitchCat"}
	FONT_NAMES = []string{"Noto Sans Bold", "Noto Sans Regular", "DejaVu Sans", "Liberation Sans", "Liberation Serif"}
	FONT_PATHS = map[string]string{
		"Noto Sans Bold":    "/usr/share/fonts/noto-cjk/NotoSansCJK-Bold.ttc",
		"Noto Sans Regular": "/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
		"DejaVu Sans":       "/usr/share/fonts/TTF/DejaVuSans.ttf",
		"Liberation Sans":   "/usr/share/fonts/liberation/LiberationSans-Regular.ttf",
		"Liberation Serif":  "/usr/share/fonts/liberation/LiberationSerif-Regular.ttf",
	}
	selectedFont  = "Noto Sans Bold"
	DEFAULT_TEXTS = []string{"うおw", "うま", "うっま", "あ", "www", "wwww", "wwwww", "wwwww", "こっから勝・つ・ぞ！オイ！💃", "んん〜まかｧｧウｯｯ!!!!🤏😎", "うおおおおおおおおお", "きたあああああああ", "いいね"}
	DEFAULT_NEXT  = []string{"あ）"}
	DEFAULT_FOLLOWS = []string{"hanjoudesu", "bijusan", "oniyadayo", "dmf_kyochan", "vodkavdk", "lazvell", "ade3_3", "goroujp", "batora324", "kato_junichi0817", "crowfps__", "gon_vl", "yuyuta0702"}
)

type V2Device struct {
	file       *os.File
	cb         func(int, bool)
	mu         sync.Mutex
	closed     bool
	prevImages []string
	virtualDir string
}

var (
	sdeck      *V2Device
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

	profCache       = NewLRU(50)
	httpClient      = &http.Client{Timeout: 10 * time.Second}
	fontRegular     *truetype.Font
	fontSmall       *truetype.Font
	otFont          *sfnt.Font
	otFontData      []byte
	scrollMode      = "title"
	profDir         string
	followCachePath string
	lastOnlineCount int
	titleWrapped    = map[string]bool{}
	catWrapped      = map[string]bool{}
	prevOnline      map[string]bool
	prevOnlineMu    sync.RWMutex
	logAnalyzer     *LogAnalyzer
	tokenManager    *TokenManager
	debugMode       = false
)

type stackEntry struct{ page, ctx string }

type LRUCache struct {
	mu   sync.Mutex
	data map[string]interface{}
	keys []string
	max  int
}

func NewLRU(max int) *LRUCache {
	return &LRUCache{data: make(map[string]interface{}), max: max}
}

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

func init() {
	userCache, err := os.UserCacheDir()
	if err != nil {
		userCache = os.TempDir()
	}
	cacheDir := filepath.Join(userCache, "streamdeck-twitch")
	profDir = filepath.Join(cacheDir, "profiles")
	followCachePath = filepath.Join(cacheDir, "followed.json")
	os.MkdirAll(profDir, 0755)
	logf("INFO", "キャッシュディレクトリ: %s", cacheDir)
}

func main() {
	loadEnv()

	if len(os.Args) > 1 && os.Args[1] == "--auto-fix" {
		infoLog("自動修正モードで起動")
		if !RunWithAutoFix() {
			errorLog("自動修正モードで起動失敗")
			os.Exit(1)
		}
		return
	}

	if !checkAndSetupConfig() {
		errorLog("Configuration not complete. Edit config file and restart.")
		os.Exit(1)
	}

	logAnalyzer = NewLogAnalyzer()
	logf("INFO", "[LOG ANALYZER] Log monitoring started")

	tokenManager = NewTokenManager()
	initToken()

	loadFonts()

	if len(os.Args) > 1 && os.Args[1] == "--virtual" {
		runVirtual()
		return
	}

	var err error
	if sdeck, err = openStreamDeck(); err != nil {
		log.Fatalf("[ERROR] Stream Deck: %v", err)
	}
	defer sdeck.Close()

	sdeck.cb = func(idx int, pressed bool) { if pressed { onKey(idx, true) } }
	go sdeck.readLoop()
	sdeck.SetBrightness(brightness)

	if AT == "" {
		show(HOME, "", false)
		logAnalyzer.LogError("NO_TOKEN", "Access Token not set at startup")
		infoLog("Access Token not set. Start authentication from OAuth button on HOME page.")
	} else {
		show(TW, "", false)
	}

	initFollows()
	fetchUsers(followed)
	prevOnline = loadPrevOnlineState()

	go bgLoop()
	go ircLoop()
	mainLoop()
}

func loadEnv() {
	CID = getEnv("TWITCH_CLIENT_ID", "")
	CS = getEnv("TWITCH_CLIENT_SECRET", "")
	SCOPE = getEnv("TWITCH_SCOPE", "user:read:email user:read:follows user:read:broadcast user:write:chat chat:read")
	AT = os.Getenv("TWITCH_ACCESS_TOKEN")
	RT = os.Getenv("TWITCH_REFRESH_TOKEN")
	UID = os.Getenv("TWITCH_USER_ID")
}

func initToken() {
	token, err := tokenManager.LoadToken()
	if err != nil {
		return
	}
	valid, reason := tokenManager.ValidateToken()
	if !valid {
		logf("WARN", "Token validation failed: %s", reason)
		logAnalyzer.LogTokenError("VALIDATION_FAILED", reason)
		AT = ""
		return
	}
	AT, RT, UID, CID = token.AccessToken, token.RefreshToken, token.UserID, token.ClientID
	logf("INFO", "Using valid token for user: %s (%s)", token.DisplayName, token.LoginName)
	logf("DEBUG", "Token scope: %s (preserved: %s)", token.Scope, SCOPE)
	tokenManager.UpdateLastUsed()

	if cfg := loadConfigFromFile(); cfg.Scope != "" {
		SCOPE = cfg.Scope
		logf("INFO", "Reset SCOPE from config: %s", SCOPE)
	}
}

func runVirtual() {
	infoLog("仮想モード起動（デバイスなし）")
	outDir := "/home/nifuramu/Desktop/streamdeck_test"
	os.RemoveAll(outDir)
	os.MkdirAll(outDir, 0755)
	sdeck = &V2Device{virtualDir: outDir, prevImages: make([]string, MAX_KEYS)}
	p := HOME
	if len(os.Args) > 2 && os.Args[2] == "tw" {
		p = TW
	}
	show(p, "", false)
	infoLog("ページ %s のテスト画像を %s に出力しました", p, outDir)
}

func initFollows() {
	cached := loadFollowedFromCache()
	api := fetchFollowedFromAPI()
	switch {
	case len(api) > 0:
		followed = api
		saveFollowedToCache(api)
	case len(cached) > 0:
		followed = cached
		infoLog("APIからフォローリストを取得できなかったため、キャッシュを使用します")
	default:
		followed = DEFAULT_FOLLOWS
		infoLog("キャッシュもAPIも利用できないため、デフォルトのフォローリストを使用します")
	}
}

// --- Device ---

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

func (s *V2Device) FillImage(idx int, img image.Image) {
	if s.virtualDir != "" {
		s.saveVirtual(idx, img)
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

func (s *V2Device) saveVirtual(idx int, img image.Image) {
	dst := image.NewRGBA(image.Rect(0, 0, 72, 72))
	for y := 0; y < 72; y++ {
		for x := 0; x < 72; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			off := dst.PixOffset(x, y)
			dst.Pix[off+0] = uint8(r / 257)
			dst.Pix[off+1] = uint8(g / 257)
			dst.Pix[off+2] = uint8(b / 257)
			dst.Pix[off+3] = uint8(a / 257)
		}
	}
	fname := filepath.Join(s.virtualDir, fmt.Sprintf("btn%d.png", idx))
	if f, err := os.Create(fname); err == nil {
		png.Encode(f, dst)
		f.Close()
		if debugMode {
			logf("DEBUG", "[Virtual] Saved button %d to %s", idx, fname)
		}
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
		if buf[0] != 0x01 {
			continue
		}
		current := buf[4 : 4+MAX_KEYS]
		for i := 0; i < MAX_KEYS; i++ {
			if current[i] != prev[i] && s.cb != nil {
				s.cb(i, current[i] == 1)
			}
		}
		copy(prev, current)
	}
}

func (s *V2Device) SetBrightness(percent int) {
	payload := make([]byte, 32)
	payload[0], payload[1], payload[2] = 0x03, 0x08, byte(percent)
	if err := platformSetBrightness(s.file.Fd(), payload); err != nil {
		logf("ERROR", "[Brightness] %v", err)
	}
}

// --- Input Handling ---

func onKey(k int, p bool) {
	lastInput = time.Now()
	if logAnalyzer != nil && p {
		logAnalyzer.LogButtonPress(page, k, getButtonLabel(page, k))
	}
	handlers[page](k, p)
}

type keyHandler func(k int, p bool)

var handlers = map[string]keyHandler{
	HOME: handleHome,
	TW:   handleTW,
	LV:   handleLV,
	TX:   handleTX,
	NX:   handleNX,
	ST:   handleST,
	SD:   handleSD,
	OA:   handleOA,
	FN:   handleFN,
	UI:   handleUI,
}

func handleNav(k int, home, back bool) bool {
	if home && k == 13 {
		show(HOME, "", false)
		return true
	}
	if back && k == 14 {
		back()
		return true
	}
	return false
}

func handleHome(k int, p bool) {
	switch k {
	case 0: show(TW, "", true)
	case 1: show(OA, "", true)
	case 2: show(ST, "", true)
	}
}

func handleTW(k int, p bool) {
	if handleNav(k, false, true) {
		return
	}
	if k < len(twOrder) {
		show(LV, twOrder[k], true)
	}
}

func handleLV(k int, p bool) {
	if handleNav(k, true, true) {
		return
	}
	switch {
	case k < len(EMOTES) && live != "":
		ircSend(live, EMOTES[k])
	case k == 11 && live != "":
		platformOpenBrowser("https://www.twitch.tv/" + live)
	case k == 12:
		show(TX, live, true)
	}
}

func handleTX(k int, p bool) {
	if handleNav(k, true, true) {
		return
	}
	if k < len(DEFAULT_TEXTS) && live != "" {
		ircSend(live, DEFAULT_TEXTS[k])
	} else if k == 12 {
		show(NX, live, true)
	}
}

func handleNX(k int, p bool) {
	if handleNav(k, true, true) {
		return
	}
	if k < len(DEFAULT_NEXT) && live != "" {
		ircSend(live, DEFAULT_NEXT[k])
	}
}

func handleST(k int, p bool) {
	if handleNav(k, true, false) {
		return
	}
	switch k {
	case 0: show(SD, "", true)
	case 1: platformReboot()
	case 2: show(FN, "", true)
	case 3: show(UI, "", true)
	}
}

func handleSD(k int, p bool) {
	if handleNav(k, true, true) {
		return
	}
	switch k {
	case 0:
		brightness = min(100, brightness+10)
		sdeck.SetBrightness(brightness)
		renderSD()
	case 1:
		brightness = max(0, brightness-10)
		sdeck.SetBrightness(brightness)
		renderSD()
	}
}

func handleOA(k int, p bool) {
	if handleNav(k, false, true) {
		return
	}
	if k == 0 {
		startOAuth()
	}
}

func handleFN(k int, p bool) {
	if handleNav(k, true, true) {
		return
	}
	if k >= len(FONT_NAMES) {
		return
	}
	name := FONT_NAMES[k]
	infoLog("Font selected: %s", name)
	selectedFont = name
	if path, ok := FONT_PATHS[name]; ok {
		platformSetFontPath(path)
		loadFonts()
		show(HOME, "", false)
	}
}

func handleUI(k int, p bool) {
	if handleNav(k, true, true) {
		return
	}
	switch k {
	case 0: infoLog("下部背景高さ調整")
	case 1: infoLog("視聴数背景余白調整")
	case 2: infoLog("配信時間背景余白調整")
	}
}

func getButtonLabel(page string, idx int) string {
	switch page {
	case LV:
		if idx < len(EMOTES) {
			return EMOTES[idx]
		}
	case TW:
		if idx < len(twOrder) {
			return twOrder[idx]
		}
	}
	labels, ok := pageLabels[page]
	if !ok {
		return fmt.Sprintf("Page:%s Btn:%d", page, idx)
	}
	if l, ok := labels[idx]; ok {
		return l
	}
	return fmt.Sprintf("Button %d", idx)
}

var pageLabels = map[string]map[int]string{
	HOME: {0: "Twitch", 1: "OAuth", 2: "Setting"},
	OA:   {0: "Auth", 14: "Back"},
	ST:   {0: "StreamDeck", 1: "再起動", 14: "ホーム"},
	SD:   {0: "明るさUP", 1: "明るさDW", 13: "ホーム", 14: "戻る"},
}

// --- Utils ---

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func logf(level, format string, args ...interface{}) {
	log.Printf("["+level+"] "+format, args...)
}

func debugLog(format string, args ...interface{}) {
	if debugMode {
		logf("DEBUG", format, args...)
	}
}

func infoLog(format string, args ...interface{})  { logf("INFO", format, args...) }
func warnLog(format string, args ...interface{})  { logf("WARN", format, args...) }
func errorLog(format string, args ...interface{}) { logf("ERROR", format, args...) }

func showTokenError(msg string) {
	logf("TOKEN ERROR", "%s", msg)
	if logAnalyzer != nil {
		logAnalyzer.LogTokenError("ERROR", msg)
	}
	if page != HOME {
		show(HOME, "", false)
		infoLog("Please use OAuth button to re-authenticate")
	}
}

// --- IRC ---
var (
	ircConn          net.Conn
	ircMu            sync.Mutex
	ircJoined        = make(map[string]bool)
	ircUsername      string
	ircUsernameTries int
	lastPing         time.Time
)

// fetchIRCUsername fetches the Twitch username from the API
// Returns true if successful, false otherwise