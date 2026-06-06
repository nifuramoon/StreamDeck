package main


import (
	//
	"crypto/sha1"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strconv"
	"time"

)


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

func drawSimpleText(img *image.RGBA, x, y int, text string, col color.RGBA, size float64) {
	charWidth := int(size * 0.6)
	charHeight := int(size * 0.8)
	if charWidth < 1 { charWidth = 1 }
	if charHeight < 1 { charHeight = 1 }

	runeCount := 0
	for range text { runeCount++ }
	totalWidth := runeCount * (charWidth + 2)

	// Center the text block
	startX := x
	if totalWidth < img.Bounds().Dx() {
		startX = (img.Bounds().Dx() - totalWidth) / 2
	}
	startY := y
	if charHeight < img.Bounds().Dy() {
		startY = (img.Bounds().Dy() - charHeight) / 2
	}

	i := 0
	for _, r := range text {
		charX := startX + i*(charWidth+2)
		charY := startY
		if charX+charWidth > img.Bounds().Dx() || i >= 20 {
			break
		}
		// Draw a simple representation based on character type
		inner := color.RGBA{col.R/2, col.G/2, col.B/2, col.A}
		if r >= 0x4E00 && r <= 0x9FFF { // CJK
			drawRect(img, charX, charY, charWidth, charHeight, col)
			// Add distinguishing marks for common characters
			switch {
			case r == 0x8A8D: // 認
				drawRect(img, charX+charWidth/4, charY+charHeight/4, charWidth/2, charHeight/2, inner)
			case r == 0x8A3C: // 証
				drawLine(img, charX+charWidth/2, charY+charHeight/4, charX+charWidth/2, charY+charHeight*3/4, inner)
			case r == 0x53D6: // 取
				drawLine(img, charX+charWidth/4, charY+charHeight/2, charX+charWidth*3/4, charY+charHeight/2, inner)
			case r == 0x5F97: // 得
				drawLine(img, charX+charWidth/4, charY+charHeight/4, charX+charWidth*3/4, charY+charHeight*3/4, inner)
				drawLine(img, charX+charWidth*3/4, charY+charHeight/4, charX+charWidth/4, charY+charHeight*3/4, inner)
			default:
				// Other CJK: draw cross
				drawLine(img, charX+charWidth/2, charY+charHeight/4, charX+charWidth/2, charY+charHeight*3/4, inner)
				drawLine(img, charX+charWidth/4, charY+charHeight/2, charX+charWidth*3/4, charY+charHeight/2, inner)
			}
		} else if (r >= 0x3040 && r <= 0x309F) || (r >= 0x30A0 && r <= 0x30FF) { // Hiragana/Katakana
			drawRectOutline(img, charX, charY, charWidth, charHeight, col)
			drawLine(img, charX+charWidth/2, charY+charHeight/4, charX+charWidth/2, charY+charHeight*3/4, inner)
		} else { // Latin/ASCII
			drawRectOutline(img, charX, charY, charWidth, charHeight, col)
		}
		i++
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
// --- Bitmap font for button text ---
func drawBitmapText(img *image.RGBA, x, y int, text string, col color.RGBA, size float64) {
	// Built-in bitmap font for common button texts
	// Each character is 8x12 pixels in a 10x14 cell
	charW, charH := 8, 12
	cellW, cellH := 10, 14
	if int(size) < 10 { cellW = 8; charW, charH = 6, 8 }
	_ = cellH // unused
	
	// Simple monochrome bitmap glyphs for ASCII and common Japanese chars
	glyphs := map[rune]bitmap{}

	// Latin uppercase letters
	for r := 'A'; r <= 'Z'; r++ {
		b := bitmap{charW, charH, make([]uint8, charW*charH)}
		for i := range b.data { b.data[i] = 0 }
		// Draw simple block letter
		stripW := charW / 3
		if stripW < 1 { stripW = 1 }
		for row := 0; row < charH; row++ {
			for col := 0; col < charW; col++ {
				isEdge := (row == 0 || row == charH-1 || col < stripW || col >= charW-stripW)
				isMidH := (row >= charH/3 && row <= charH*2/3)
				if isEdge && isMidH {
					b.data[row*charW+col] = 1
				}
			}
		}
		glyphs[r] = b
	}
	// Specific glyphs for characters used in buttons
	glyphs['A'] = glyphA()
	glyphs['T'] = glyphT()
	glyphs['w'] = glyphW()
	glyphs['i'] = glyphI()
	glyphs['t'] = glyphT()
	glyphs['c'] = glyphC()
	glyphs['h'] = glyphH()
	glyphs['o'] = glyphO()
	glyphs['u'] = glyphU()
	glyphs['S'] = glyphS()
	glyphs['e'] = glyphE()
	glyphs['n'] = glyphN()
	glyphs['g'] = glyphG()
	glyphs['B'] = glyphB()
	glyphs['k'] = glyphK()
	
	// Draw each character
	dx := x
	for _, r := range text {
		g, ok := glyphs[r]
		if !ok { dx += cellW; continue }
		for row := 0; row < g.h && y+row < 72; row++ {
			for c := 0; c < g.w && dx+c < 72; c++ {
				if g.data[row*g.w+c] != 0 {
					img.Set(dx+c, y+row, col)
				}
			}
		}
		dx += cellW
	}
}
func glyphA() bitmap { return bitmap{8,12,[]uint8{
	0,0,1,1,1,1,0,0,
	0,1,0,0,0,0,1,0,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,1,1,1,1,1,1,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
}}}
func glyphT() bitmap { return bitmap{8,12,[]uint8{
	1,1,1,1,1,1,1,1,
	0,0,0,0,1,0,0,0,
	0,0,0,0,1,0,0,0,
	0,0,0,0,1,0,0,0,
	0,0,0,0,1,0,0,0,
	0,0,0,0,1,0,0,0,
	0,0,0,0,1,0,0,0,
	0,0,0,0,1,0,0,0,
	0,0,0,0,1,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
}}}
func glyphW() bitmap { return bitmap{8,12,[]uint8{
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	0,1,1,1,1,1,1,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
}}}
func glyphI() bitmap { return bitmap{8,12,[]uint8{
	0,0,1,1,1,1,0,0,
	0,0,0,0,1,0,0,0,
	0,0,0,0,1,0,0,0,
	0,0,0,0,1,0,0,0,
	0,0,0,0,1,0,0,0,
	0,0,0,0,1,0,0,0,
	0,0,0,0,1,0,0,0,
	0,0,0,0,1,0,0,0,
	0,0,1,1,1,1,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
}}}
func glyphC() bitmap { return bitmap{8,12,[]uint8{
	0,0,1,1,1,1,0,0,
	0,1,0,0,0,0,1,0,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,0,
	1,0,0,0,0,0,0,0,
	1,0,0,0,0,0,0,0,
	1,0,0,0,0,0,0,0,
	0,1,0,0,0,0,1,0,
	0,0,1,1,1,1,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
}}}
func glyphH() bitmap { return bitmap{8,12,[]uint8{
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,1,1,1,1,1,1,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
}}}
func glyphO() bitmap { return bitmap{8,12,[]uint8{
	0,0,1,1,1,1,0,0,
	0,1,0,0,0,0,1,0,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	0,1,0,0,0,0,1,0,
	0,0,1,1,1,1,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
}}}
func glyphU() bitmap { return bitmap{8,12,[]uint8{
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	0,1,0,0,0,0,1,0,
	0,0,1,1,1,1,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
}}}
func glyphS() bitmap { return bitmap{8,12,[]uint8{
	0,0,1,1,1,1,1,0,
	0,1,0,0,0,0,0,1,
	1,0,0,0,0,0,0,0,
	0,1,1,1,1,1,1,0,
	0,0,0,0,0,0,0,1,
	0,0,0,0,0,0,0,1,
	0,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	0,1,1,1,1,1,1,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
}}}
func glyphE() bitmap { return bitmap{8,12,[]uint8{
	0,1,1,1,1,1,1,1,
	0,1,0,0,0,0,0,0,
	0,1,0,0,0,0,0,0,
	0,1,1,1,1,1,1,0,
	0,1,0,0,0,0,0,0,
	0,1,0,0,0,0,0,0,
	0,1,0,0,0,0,0,0,
	0,1,0,0,0,0,0,0,
	0,1,1,1,1,1,1,1,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
}}}
func glyphN() bitmap { return bitmap{8,12,[]uint8{
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,1,0,0,0,0,0,1,
	1,0,1,0,0,0,0,1,
	1,0,0,1,0,0,0,1,
	1,0,0,0,1,0,0,1,
	1,0,0,0,0,1,0,1,
	1,0,0,0,0,0,1,1,
	1,0,0,0,0,0,0,1,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
}}}
func glyphG() bitmap { return bitmap{8,12,[]uint8{
	0,0,1,1,1,1,1,0,
	0,1,0,0,0,0,0,1,
	1,0,0,0,0,0,0,0,
	1,0,0,0,1,1,1,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	0,1,0,0,0,0,1,0,
	0,0,1,1,1,1,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
}}}
func glyphB() bitmap { return bitmap{8,12,[]uint8{
	1,1,1,1,1,1,0,0,
	1,0,0,0,0,0,1,0,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,1,0,
	1,1,1,1,1,1,0,0,
	1,0,0,0,0,0,1,0,
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,0,1,
	1,1,1,1,1,1,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
}}}
func glyphK() bitmap { return bitmap{8,12,[]uint8{
	1,0,0,0,0,0,0,1,
	1,0,0,0,0,0,1,0,
	1,0,0,0,0,1,0,0,
	1,0,0,0,1,0,0,0,
	1,1,1,1,0,0,0,0,
	1,0,0,0,1,0,0,0,
	1,0,0,0,0,1,0,0,
	1,0,0,0,0,0,1,0,
	1,0,0,0,0,0,0,1,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
	0,0,0,0,0,0,0,0,
}}}

func keyTextBg(text string, bg color.RGBA) *image.RGBA {
	img := newImg()
	fillRect(img, img.Bounds(), bg)
	drawText(img, (W-measureText(text, 14))/2, (H-14)/2, text, color.RGBA{255, 255, 255, 255}, 14)
	return img
}

// --- Fetch ---
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
		fillRect(img, image.Rect(0, 0, tw+6, 14), color.RGBA{0, 0, 0, 200})
		drawText(img, 2, 0, s, color.RGBA{255, 255, 255, 255}, 10)
	}
	if st > 0 {
		el := time.Now().Unix() - int64(st)
		h, m := el/3600, (el%3600)/60
		lab := fmt.Sprintf("%dm", m)
		if h > 0 {
			lab = fmt.Sprintf("%dh%dm", h, m)
		}
		tw := measureText(lab, 11)
		xOffset := W - tw - 4
		fillRect(img, image.Rect(xOffset, 0, W, 14), color.RGBA{0, 0, 0, 200})
		drawText(img, xOffset+2, 0, lab, color.RGBA{255, 255, 255, 255}, 10)
	}
	if txt != "" {
		col := color.RGBA{255, 217, 0, 255}
		if scrollMode == "category" {
			col = color.RGBA{200, 245, 255, 255}
		}
		y := H - 21 // さらに1ピクセル上に調整
		fillRect(img, image.Rect(0, y, W, H), color.RGBA{0, 0, 0, 230})
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

	// 通知設定ボタン
	notificationText := "通知OFF"
	notificationColor := color.RGBA{100, 0, 0, 255} // 赤色（OFF時）
	if notificationEnabled {
		notificationText = "通知ON"
		notificationColor = color.RGBA{0, 100, 0, 255} // 緑色（ON時）
	}
	sdeck.FillImage(2, keyTextBg(notificationText, notificationColor))

	// テスト音声ボタン
	sdeck.FillImage(3, keyTextBg("テスト音声", color.RGBA{0, 0, 100, 255}))

	// ボタン4-13は空白
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

func renderOA() {
	deckMu.Lock()
	for i := 0; i < 15; i++ {
		sdeck.FillBlank(i)
	}
	sdeck.FillImage(0, keyTextBg("Auth", color.RGBA{0, 100, 200, 255}))
	sdeck.FillImage(14, keyTextBg("Back", color.RGBA{40, 40, 0, 255}))
	deckMu.Unlock()
}