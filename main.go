package main

import (
	"bytes"
	"crypto/sha1"
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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
)

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
	HOME, TW, LV, TX, NX, ST, SD, M32 = "home", "tw", "lv", "tx", "nx", "st", "sd", "m32"
)

var (
	CID   = os.Getenv("TWITCH_CLIENT_ID")
	CS    = os.Getenv("TWITCH_CLIENT_SECRET")
	AT    = os.Getenv("TWITCH_ACCESS_TOKEN")
	RT    = os.Getenv("TWITCH_REFRESH_TOKEN")
	UID   = os.Getenv("TWITCH_USER_ID")
	IRC_T = os.Getenv("TWITCH_IRC_TOKEN")
)

// 仕様書に基づく厳選されたスタンプとテキスト
var EMOTES = []string{"BloodTrail", "HeyGuys", "LUL", "DinoDance", "HungryPaimon", "GlitchCat"}
var DEFAULT_TEXTS = []string{"うおw", "こっから勝・つ・ぞ！オイ！💃", "んん〜まかｧｧウｯｯ!!!!🤏😎"}
var DEFAULT_NEXT = []string{"あ）"}

// --- Globals ---
var (
	sdeck         *V2Device
	deckMu        sync.Mutex
	page          = TW
	stack         []stackEntry
	live          string
	brightness    = 50
	lastInput     = time.Now()

	stateMu      sync.RWMutex
	followed     []string
	lu           = map[string]map[string]interface{}{}
	id2lg        = map[string]string{}
	twOrder      []string
	views        = map[string]int{}
	startedAt    = map[string]float64{}
	titles       = map[string]string{}
	titleOfs     = map[string]float64{}
	titleStep    = map[string]float64{}
	categories   = map[string]string{}
	catOfs       = map[string]float64{}
	catStep      = map[string]float64{}
	titleW       = map[string]float64{}
	catW         = map[string]float64{}
	scrollMode   = "title"
	titleWrapped = map[string]bool{}
	catWrapped   = map[string]bool{}

	profDir         string
	fontRegular     *truetype.Font
	fontSmall       *truetype.Font
	httpClient      = &http.Client{Timeout: 10 * time.Second}
	profCache       = NewLRU(200)
	lastOnlineCount = -1
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
	c.mu.Lock(); defer c.mu.Unlock(); v, ok := c.data[key]; return v, ok
}
func (c *LRUCache) Set(key string, val interface{}) {
	c.mu.Lock(); defer c.mu.Unlock()
	if _, ok := c.data[key]; !ok { c.keys = append(c.keys, key) }
	c.data[key] = val
	if len(c.keys) > c.max { delete(c.data, c.keys[0]); c.keys = c.keys[1:] }
}

// --- Device Control ---
type V2Device struct {
	file       *os.File
	cb         func(int, bool)
	closed     bool
	prevImages [MAX_KEYS]string
}
func (s *V2Device) ClearAllBtns() { for i := 0; i < MAX_KEYS; i++ { s.FillBlank(i) } }
func (s *V2Device) Close() { s.closed = true; if s.file != nil { s.file.Close() } }
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
		for x := 0; x < w; x++ { res.Set(w-1-x, h-1-y, img.At(x, y)) }
	}
	return res
}
func (s *V2Device) FillImage(idx int, img image.Image) {
	flipped := flipV2(img)
	var buf bytes.Buffer
	jpeg.Encode(&buf, flipped, &jpeg.Options{Quality: 90})
	payload := buf.Bytes()

	hash := fmt.Sprintf("%x", sha1.Sum(payload))
	if s.prevImages[idx] == hash { return }
	s.prevImages[idx] = hash

	pageNum, sent := 0, 0
	for sent < len(payload) {
		chunkSz := len(payload) - sent
		if chunkSz > V2_ITER_SZ { chunkSz = V2_ITER_SZ }
		isLast := 0
		if sent+chunkSz == len(payload) { isLast = 1 }
		header := make([]byte, V2_HEADER_SZ)
		header[0], header[1], header[2], header[3] = 0x02, 0x07, byte(idx), byte(isLast)
		header[4], header[5] = byte(chunkSz&0xFF), byte(chunkSz>>8)
		header[6], header[7] = byte(pageNum&0xFF), byte(pageNum>>8)
		packet := make([]byte, V2_PAGE_PACKET_SZ)
		copy(packet, header)
		copy(packet[V2_HEADER_SZ:], payload[sent:sent+chunkSz])
		s.file.Write(packet)
		sent += chunkSz; pageNum++
	}
}
func (s *V2Device) readLoop() {
	prev := make([]byte, MAX_KEYS)
	for !s.closed {
		buf := make([]byte, 32)
		n, err := s.file.Read(buf)
		if err != nil || n < 4 { time.Sleep(10 * time.Millisecond); continue }
		if buf[0] == 0x01 {
			current := buf[4 : 4+MAX_KEYS]
			for i := 0; i < MAX_KEYS; i++ {
				if current[i] != prev[i] && s.cb != nil { s.cb(i, current[i] == 1) }
			}
			copy(prev, current)
		}
	}
}
func (s *V2Device) SetBrightness(percent int) {
	payload := make([]byte, 32)
	payload[0], payload[1], payload[2] = 0x03, 0x08, byte(percent)
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, s.file.Fd(), 0xC0204806, uintptr(unsafe.Pointer(&payload[0])))
	if err != 0 { log.Printf("[Brightness] ioctl error: %v", err) }
}

// --- Entry Point ---
func init() {
	// 【完全グローバル絶対パス】 ~/.cache/streamdeck-twitch を取得
	userCache, err := os.UserCacheDir()
	if err != nil { userCache = "/tmp" } // 万が一取得できなかった場合のフォールバック
	
	cacheDir := filepath.Join(userCache, "streamdeck-twitch")
	profDir = filepath.Join(cacheDir, "profiles")
	os.MkdirAll(profDir, 0755)
	
	log.Printf("[INFO] 効率化ディレクトリ(Global Cache)を設定: %s\n", cacheDir)
}

func main() {
	if CID == "" || AT == "" { log.Fatalf("[ERROR] TWITCH_CLIENT_ID or TWITCH_ACCESS_TOKEN not set") }
	loadFonts()
	var err error
	if sdeck, err = openStreamDeck(); err != nil { log.Fatalf("[ERROR] Stream Deck: %v", err) }
	defer sdeck.Close()
	
	sdeck.cb = func(idx int, pressed bool) { if pressed { onKey(idx, true) } }
	go sdeck.readLoop()
	
	sdeck.SetBrightness(brightness)
	show(TW, "", false)

	apiFollows := fetchFollowedFromAPI()
	if len(apiFollows) > 0 { followed = apiFollows } else {
		followed = []string{"hanjoudesu", "bijusan", "oniyadayo", "dmf_kyochan", "vodkavdk", "lazvell", "ade3_3", "goroujp", "batora324", "kato_junichi0817", "crowfps__", "gon_vl", "yuyuta0702"}
	}
	
	fetchUsers(followed)
	go bgLoop()
	go ircLoop()
	mainLoop()
}

func openStreamDeck() (*V2Device, error) {
	for attempt := 0; attempt < 10; attempt++ {
		files, err := os.ReadDir("/sys/class/hidraw")
		if err == nil {
			for _, f := range files {
				uevent, err := os.ReadFile(filepath.Join("/sys/class/hidraw", f.Name(), "device", "uevent"))
				if err != nil { continue }
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

// --- Utils & Graphics ---
func hsh(s string) string { return fmt.Sprintf("%x", sha1.Sum([]byte(s))) }
func newImg() *image.RGBA { return image.NewRGBA(image.Rect(0, 0, W, H)) }
func fillRect(img *image.RGBA, r image.Rectangle, c color.RGBA) { draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Src) }
func min(a, b int) int { if a < b { return a }; return b }
func max(a, b int) int { if a > b { return a }; return b }

func resize72(src image.Image) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, 72, 72))
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < 72; y++ {
		for x := 0; x < 72; x++ { dst.Set(x, y, src.At(src.Bounds().Min.X+(x*w/72), src.Bounds().Min.Y+(y*h/72))) }
	}
	return dst
}

func loadFonts() {
	candidates := []string{
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Bold.ttc",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Bold.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/ipafont/ipag.ttf",
		"/usr/share/fonts/truetype/fonts-japanese-gothic.ttf",
	}
	for _, p := range candidates {
		if data, err := os.ReadFile(p); err == nil {
			if f, err := freetype.ParseFont(data); err == nil {
				fontRegular, fontSmall = f, f
				log.Printf("[Font] Loaded: %s", p)
				return
			}
		}
	}
}
func measureText(text string, size float64) int {
	if fontRegular == nil { return len(text) * int(size) }
	face := truetype.NewFace(fontRegular, &truetype.Options{Size: size, DPI: 72})
	defer face.Close(); return font.MeasureString(face, text).Ceil()
}
func drawText(img *image.RGBA, x, y int, text string, col color.RGBA, size float64) {
	if fontRegular == nil { return }
	c := freetype.NewContext(); c.SetDPI(72); c.SetFont(fontRegular); c.SetFontSize(size)
	c.SetClip(img.Bounds()); c.SetDst(img); c.SetSrc(image.NewUniform(col))
	c.DrawString(text, freetype.Pt(x, y+int(size)))
}
func keyTextBg(text string, bg color.RGBA) *image.RGBA {
	img := newImg(); fillRect(img, img.Bounds(), bg)
	drawText(img, (W-measureText(text, 14))/2, (H-14)/2, text, color.RGBA{255, 255, 255, 255}, 14)
	return img
}

// --- Fetch ---
func fetchProf(u string) image.Image {
	if v, ok := profCache.Get(u); ok { return v.(image.Image) }
	p := filepath.Join(profDir, hsh(u)+".jpg")
	if f, err := os.Open(p); err == nil { 
		defer f.Close(); 
		if img, err := jpeg.Decode(f); err == nil { 
			resized := resize72(img)
			profCache.Set(u, resized); return resized 
		} 
	}
	resp, err := httpClient.Get(u)
	if err != nil { return nil }
	defer resp.Body.Close(); data, _ := io.ReadAll(resp.Body)
	os.WriteFile(p, data, 0644)
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil { img, err = png.Decode(bytes.NewReader(data)) }
	if err == nil { 
		resized := resize72(img)
		profCache.Set(u, resized); return resized 
	}
	return nil
}

func twitchGet(u string, params url.Values) map[string]interface{} {
	if params != nil { u += "?" + params.Encode() }
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Client-ID", CID); req.Header.Set("Authorization", "Bearer "+AT)
	resp, err := httpClient.Do(req)
	
	if err != nil { 
		log.Printf("[API ERROR] 通信失敗 URL: %s, 理由: %v", u, err)
		return map[string]interface{}{} 
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 && RT != "" && CS != "" {
		log.Println("[API WARN] トークン有効期限切れ(HTTP 401)を検知。リフレッシュを試行します...")
		refReq, _ := http.NewRequest("POST", "https://id.twitch.tv/oauth2/token", strings.NewReader(url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {RT},
			"client_id":     {CID},
			"client_secret": {CS},
		}.Encode()))
		refReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		refResp, err := httpClient.Do(refReq)
		if err != nil {
			log.Printf("[API ERROR] リフレッシュ通信失敗: %v", err)
		} else {
			defer refResp.Body.Close()
			var refRes map[string]interface{}
			json.NewDecoder(refResp.Body).Decode(&refRes)
			if newToken, ok := refRes["access_token"].(string); ok {
				AT = newToken
				log.Println("[API INFO] トークンのリフレッシュに成功しました！新しいトークンを適用します。")
				req.Header.Set("Authorization", "Bearer "+AT)
				retryResp, err := httpClient.Do(req)
				if err != nil {
					log.Printf("[API ERROR] リフレッシュ後の再リクエスト失敗: %v", err)
					return map[string]interface{}{}
				}
				defer retryResp.Body.Close()
				if retryResp.StatusCode >= 400 {
					return map[string]interface{}{}
				}
				var retryRes map[string]interface{}
				json.NewDecoder(retryResp.Body).Decode(&retryRes)
				return retryRes
			}
		}
	} else if resp.StatusCode >= 400 {
		return map[string]interface{}{}
	}

	var res map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&res)
	if res == nil { res = map[string]interface{}{} }
	return res
}

func fetchFollowedFromAPI() []string {
	if UID == "" { return nil }
	var out []string
	cursor := ""
	for {
		params := url.Values{"user_id": {UID}, "first": {"100"}}
		if cursor != "" { params.Set("after", cursor) }
		js := twitchGet("https://api.twitch.tv/helix/channels/followed", params)
		data, ok := js["data"].([]interface{})
		if !ok || len(data) == 0 { break }
		for _, item := range data {
			m, _ := item.(map[string]interface{})
			lg := strings.ToLower(fmt.Sprintf("%v", m["broadcaster_login"]))
			if lg != "" { out = append(out, lg) }
		}
		pag, _ := js["pagination"].(map[string]interface{})
		cursor, _ = pag["cursor"].(string)
		if cursor == "" { break }
	}
	return out
}

func fetchUsers(logins []string) {
	for i := 0; i < len(logins); i += 100 {
		end := min(i+100, len(logins))
		params := url.Values{}
		for _, l := range logins[i:end] { params.Add("login", l) }
		js := twitchGet("https://api.twitch.tv/helix/users", params)
		if data, ok := js["data"].([]interface{}); ok {
			for _, item := range data {
				u := item.(map[string]interface{})
				lg := strings.ToLower(fmt.Sprintf("%v", u["login"]))
				stateMu.Lock(); lu[lg] = u; id2lg[fmt.Sprintf("%v", u["id"])] = lg; stateMu.Unlock()
			}
		}
	}
}

func fetchStreams() {
	if len(followed) == 0 { return }
	stateMu.RLock(); var uids []string; for _, f := range followed { if u, ok := lu[f]; ok { uids = append(uids, fmt.Sprintf("%v", u["id"])) } }; stateMu.RUnlock()
	
	var online []map[string]interface{}
	hasError := false

	for i := 0; i < len(uids); i += 100 {
		end := min(i+100, len(uids)); params := url.Values{}
		for _, uid := range uids[i:end] { params.Add("user_id", uid) }
		js := twitchGet("https://api.twitch.tv/helix/streams", params)
		
		if data, ok := js["data"].([]interface{}); ok {
			for _, item := range data { online = append(online, item.(map[string]interface{})) }
		} else {
			hasError = true
			break
		}
	}

	if hasError { return }

	for i := 0; i < len(online); i++ {
		for j := i + 1; j < len(online); j++ {
			if online[j]["viewer_count"].(float64) > online[i]["viewer_count"].(float64) { online[i], online[j] = online[j], online[i] }
		}
	}
	if len(online) > MAX_TWITCH_KEYS { online = online[:MAX_TWITCH_KEYS] }

	stateMu.Lock()
	twOrder = nil; views = map[string]int{}; startedAt = map[string]float64{}
	for _, s := range online {
		lg := id2lg[fmt.Sprintf("%v", s["user_id"])]
		if lg == "" { continue }
		twOrder = append(twOrder, lg)
		views[lg] = int(s["viewer_count"].(float64)) // 視聴数を1単位そのままで保持
		title := fmt.Sprintf("%v", s["title"])
		titles[lg] = title; titleStep[lg] = (float64(measureText(title+"   ", 14)) / math.Max(2.0, minF(8.0, float64(len([]rune(title)))*0.2+1.5))) * SCROLL_IV
		titleW[lg] = float64(measureText(title+"   ", 14))
		game := fmt.Sprintf("%v", s["game_name"])
		categories[lg] = game; catStep[lg] = (float64(measureText(game+"   ", 14)) / math.Max(2.0, minF(8.0, float64(len([]rune(game)))*0.2+1.5))) * SCROLL_IV
		catW[lg] = float64(measureText(game+"   ", 14))
		if t, err := time.Parse(time.RFC3339, fmt.Sprintf("%v", s["started_at"])); err == nil { startedAt[lg] = float64(t.Unix()) }
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
func minF(a, b float64) float64 { if a < b { return a }; return b }

// --- Renderers ---
func renderHome() {
	deckMu.Lock()
	sdeck.FillImage(0, keyTextBg("Twitch", color.RGBA{100, 0, 255, 255}))
	sdeck.FillBlank(1) // Ahkを削除し空（0）に
	sdeck.FillImage(2, keyTextBg("Setting", color.RGBA{40, 40, 40, 255}))
	for i := 3; i < MAX_KEYS; i++ { sdeck.FillBlank(i) }
	deckMu.Unlock()
}

func twImg(profURL, login string) *image.RGBA {
	img := newImg(); prof := fetchProf(profURL)
	if prof != nil { draw.Draw(img, img.Bounds(), prof, image.Point{}, draw.Src) } else { fillRect(img, img.Bounds(), color.RGBA{30, 30, 30, 255}); drawText(img, 4, 25, login, color.RGBA{255, 255, 255, 255}, 12) }

	stateMu.RLock()
	v, st := views[login], startedAt[login]
	txt, ofs := titles[login], titleOfs[login]
	if scrollMode == "category" { txt, ofs = categories[login], catOfs[login] }
	stateMu.RUnlock()

	// 視聴数を1単位（そのままの数値）で表示
	if v > 0 {
		s := strconv.Itoa(v)
		tw := measureText(s, 11)
		fillRect(img, image.Rect(4, 3, tw+10, 18), color.RGBA{30, 35, 40, 220})
		drawText(img, 7, 4, s, color.RGBA{240, 240, 240, 255}, 11)
	}
	if st > 0 {
		el := time.Now().Unix() - int64(st); h, m := el/3600, (el%3600)/60
		lab := fmt.Sprintf("%dm", m); if h > 0 { lab = fmt.Sprintf("%dh%dm", h, m) }
		tw := measureText(lab, 11)
		xOffset := W - tw - 15 
		fillRect(img, image.Rect(xOffset, 3, xOffset+tw+6, 18), color.RGBA{30, 35, 40, 220})
		drawText(img, xOffset+3, 4, lab, color.RGBA{240, 240, 240, 255}, 11)
	}
	if txt != "" {
		col := color.RGBA{255, 217, 0, 255}; if scrollMode == "category" { col = color.RGBA{200, 245, 255, 255} }
		y := H - 20 // 下部タイトルを2ピクセル上に移動
		fillRect(img, image.Rect(0, y-2, W, H), color.RGBA{0, 0, 0, 230}); tx := txt + "   "
		if tw := float64(measureText(tx, 14)); tw > 0 {
			xp := -int(math.Mod(ofs, tw)); drawText(img, xp, y, tx, col, 14); if float64(xp)+tw < float64(W) { drawText(img, xp+int(tw), y, tx, col, 14) }
		}
	}
	return img
}

func renderTW() {
	stateMu.RLock(); order := append([]string{}, twOrder...); stateMu.RUnlock()
	deckMu.Lock()
	for i := 0; i < MAX_TWITCH_KEYS; i++ {
		if i < len(order) {
			lg := order[i]; u := lu[lg]
			if u != nil { sdeck.FillImage(i, twImg(fmt.Sprintf("%v", u["profile_image_url"]), lg)) } else { sdeck.FillImage(i, keyTextBg(lg, color.RGBA{0, 0, 0, 255})) }
		} else { sdeck.FillBlank(i) }
	}
	sdeck.FillImage(14, keyTextBg("ホーム", color.RGBA{50, 0, 50, 255}))
	deckMu.Unlock()
}

func renderLV(lg string) {
	deckMu.Lock()
	for i := 0; i < 15; i++ { sdeck.FillBlank(i) } // 全て0パディングでリセット
	
	// 厳選されたスタンプを埋め込み (0~5)
	for i := 0; i < len(EMOTES); i++ { 
		sdeck.FillImage(i, keyTextBg(EMOTES[i], color.RGBA{0,0,0,255})) 
	}
	// 下部操作ボタン
	sdeck.FillImage(11, keyTextBg("配信を見る", color.RGBA{20, 40, 20, 255}))
	sdeck.FillImage(12, keyTextBg("TEXT", color.RGBA{20, 20, 40, 255}))
	sdeck.FillImage(13, keyTextBg("ホーム", color.RGBA{0, 40, 40, 255}))
	sdeck.FillImage(14, keyTextBg("戻る", color.RGBA{40, 40, 0, 255}))
	deckMu.Unlock()
}

func renderTX() {
	deckMu.Lock()
	for i := 0; i < 15; i++ { sdeck.FillBlank(i) }
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
	for i := 0; i < 15; i++ { sdeck.FillBlank(i) }
	for i := 0; i < len(DEFAULT_NEXT); i++ { 
		sdeck.FillImage(i, keyTextBg(DEFAULT_NEXT[i], color.RGBA{30, 30, 30, 255})) 
	}
	sdeck.FillImage(13, keyTextBg("ホーム", color.RGBA{0, 40, 40, 255}))
	sdeck.FillImage(14, keyTextBg("戻る", color.RGBA{40, 40, 0, 255}))
	deckMu.Unlock()
}

func renderST() {
	deckMu.Lock()
	for i := 0; i < 15; i++ { sdeck.FillBlank(i) }
	sdeck.FillImage(0, keyTextBg("StreamDeck", color.RGBA{30, 30, 30, 255}))
	sdeck.FillImage(1, keyTextBg("再起動", color.RGBA{60, 0, 0, 255}))
	sdeck.FillImage(2, keyTextBg("32M4K", color.RGBA{30, 30, 30, 255}))
	sdeck.FillImage(14, keyTextBg("ホーム", color.RGBA{0, 40, 40, 255}))
	deckMu.Unlock()
}

func renderSD() {
	deckMu.Lock()
	for i := 0; i < 15; i++ { sdeck.FillBlank(i) }
	sdeck.FillImage(0, keyTextBg("明るさUP", color.RGBA{40, 40, 0, 255}))
	sdeck.FillImage(1, keyTextBg("明るさDW", color.RGBA{40, 0, 0, 255}))
	// 明るさの表示は仕様書で「0」指定のため削除し完全準拠
	sdeck.FillImage(13, keyTextBg("ホーム", color.RGBA{0, 40, 40, 255}))
	sdeck.FillImage(14, keyTextBg("戻る", color.RGBA{40, 40, 0, 255}))
	deckMu.Unlock()
}

func renderM32() {
	deckMu.Lock()
	for i := 0; i < 15; i++ { sdeck.FillBlank(i) }
	labels := []string{"優しい", "超優しい", "映画", "FPS", "視認性"}
	for i, l := range labels { sdeck.FillImage(i, keyTextBg(l, color.RGBA{20, 20, 20, 255})) }
	sdeck.FillImage(14, keyTextBg("ホーム", color.RGBA{0, 40, 40, 255}))
	deckMu.Unlock()
}

func show(pg, ctx string, st bool) {
	if st && page != pg { stack = append(stack, stackEntry{page, live}) }
	page, live = pg, ctx
	deckMu.Lock(); for i := 0; i < MAX_KEYS; i++ { sdeck.prevImages[i] = "" }; deckMu.Unlock() 
	switch pg {
	case HOME: renderHome()
	case TW: renderTW()
	case LV: renderLV(ctx)
	case TX: renderTX()
	case NX: renderNX()
	case ST: renderST()
	case SD: renderSD()
	case M32: renderM32()
	}
}
func back() {
	if len(stack) == 0 { show(HOME, "", false); return }
	e := stack[len(stack)-1]; stack = stack[:len(stack)-1]
	show(e.page, e.ctx, false)
}

func onKey(k int, p bool) {
	lastInput = time.Now()
	switch page {
	case HOME:
		if k == 0 { show(TW, "", true) } else if k == 2 { show(ST, "", true) }
	case TW:
		if k == 14 { show(HOME, "", false) } else if k < len(twOrder) { show(LV, twOrder[k], true) }
	case LV:
		if k < len(EMOTES) && live != "" { ircSend(live, EMOTES[k]) }
		if k == 11 && live != "" { exec.Command("xdg-open", "https://www.twitch.tv/"+live).Start() }
		if k == 12 { show(TX, live, true) }
		if k == 13 { show(HOME, "", false) }
		if k == 14 { back() }
	case TX:
		if k < len(DEFAULT_TEXTS) && live != "" { ircSend(live, DEFAULT_TEXTS[k]) }
		if k == 12 { show(NX, live, true) }
		if k == 13 { show(HOME, "", false) }
		if k == 14 { back() }
	case NX:
		if k < len(DEFAULT_NEXT) && live != "" { ircSend(live, DEFAULT_NEXT[k]) }
		if k == 13 { show(HOME, "", false) }
		if k == 14 { back() }
	case ST:
		if k == 0 { show(SD, "", true) } else if k == 1 { exec.Command("systemctl", "reboot").Start() } else if k == 2 { show(M32, "", true) }
		if k == 14 { show(HOME, "", false) }
	case SD:
		// 1割UP/DW (10%単位) に準拠
		if k == 0 { 
			brightness = min(100, brightness+10)
			sdeck.SetBrightness(brightness)
			renderSD() 
		} else if k == 1 { 
			brightness = max(0, brightness-10)
			sdeck.SetBrightness(brightness)
			renderSD() 
		}
		if k == 13 { show(HOME, "", false) } else if k == 14 { back() }
	case M32:
		if k == 14 { show(HOME, "", false) }
	}
}

// --- IRC ---
var ircConn net.Conn
func ircLoop() {
	for {
		if ircConn == nil {
			if c, err := net.Dial("tcp", "irc.chat.twitch.tv:6667"); err == nil {
				fmt.Fprintf(c, "PASS oauth:%s\r\nNICK deckuser\r\nCAP REQ :twitch.tv/tags\r\n", AT)
				ircConn = c
			} else { time.Sleep(5 * time.Second); continue }
		}
		if live != "" { fmt.Fprintf(ircConn, "JOIN #%s\r\n", live) }
		ircConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		buf := make([]byte, 4096)
		n, err := ircConn.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() { continue }
			ircConn = nil; continue
		}
		// コメント描画処理は削除し、切断防止のPINGへのPONG応答のみ残す
		for _, line := range strings.Split(string(buf[:n]), "\r\n") {
			if strings.HasPrefix(line, "PING") { fmt.Fprintf(ircConn, "PONG :tmi.twitch.tv\r\n") }
		}
	}
}
func ircSend(ch, msg string) { if ircConn != nil { fmt.Fprintf(ircConn, "PRIVMSG #%s :%s\r\n", ch, msg) } }

// --- Loops ---
func bgLoop() {
	for { fetchStreams(); time.Sleep(FETCH_IV * time.Second) }
}
func mainLoop() {
	lastFetch, lastScroll := time.Now(), time.Now()
	for {
		now := time.Now()
		if now.Sub(lastFetch).Seconds() > FETCH_IV { lastFetch = now; if page == TW { renderTW() } }
		if page != TW && now.Sub(lastInput).Seconds() > IDLE_TIMEOUT { show(TW, "", false) }
		if now.Sub(lastScroll).Seconds() > SCROLL_IV {
			lastScroll = now; stateMu.Lock()
			if scrollMode == "title" {
				allDone := true
				for _, lg := range twOrder {
					titleOfs[lg] += titleStep[lg]
					if titleW[lg] > 0 && math.Mod(titleOfs[lg], titleW[lg])+titleStep[lg] >= titleW[lg] { titleWrapped[lg] = true }
					if !titleWrapped[lg] { allDone = false }
				}
				if allDone && len(twOrder) > 0 { scrollMode = "category"; for _, lg := range twOrder { catOfs[lg] = 0; catWrapped[lg] = false } }
			} else {
				allDone := true
				for _, lg := range twOrder {
					catOfs[lg] += catStep[lg]
					if catW[lg] > 0 && math.Mod(catOfs[lg], catW[lg])+catStep[lg] >= catW[lg] { catWrapped[lg] = true }
					if !catWrapped[lg] { allDone = false }
				}
				if allDone && len(twOrder) > 0 { scrollMode = "title"; for _, lg := range twOrder { titleOfs[lg] = 0; titleWrapped[lg] = false } }
			}
			stateMu.Unlock()
			if page == TW { renderTW() }
		}
		time.Sleep(15 * time.Millisecond) 
	}
}
