package main


import (
	//
	"encoding/binary"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)


func loadFonts() {
	candidates := platformLoadFontPaths()
	debugLog("Searching for fonts in %d paths...", len(candidates))
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil { continue }
		// Try sfnt Parse first (handles TTC and TTF)
		var sf *sfnt.Font
		if strings.HasSuffix(strings.ToLower(p), ".ttc") {
			// Parse TTC collection, take font index 0 (first font)
			coll, err := sfnt.ParseCollection(data)
			if err == nil {
				sf, err = coll.Font(0)
			}
		} else {
			sf, err = sfnt.Parse(data)
		}
		if sf != nil && err == nil {
			// Verify it works by creating a face
			face, err := opentype.NewFace(sf, &opentype.FaceOptions{Size: 14, DPI: 72, Hinting: font.HintingFull})
			if err == nil {
				face.Close()
				// Also load as freetype font for legacy use
				if tf, e := freetype.ParseFont(data); e == nil {
					fontRegular, fontSmall = tf, tf
				}
				otFont = sf
				otFontData = data
				infoLog("Loaded font: %s", filepath.Base(p))
				return
			}
		}
	}
	warnLog("No usable font found. Text rendering will use fallback rectangles.")
}

// testJapaneseText tests if the font can render Japanese characters
func testJapaneseText(face font.Face) bool {
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
		return false
	}
	log.Printf("[Font Test] ✅ Font supports Japanese: %.1f%%", supportPercent)
	return true
}

// tryLoadFirstFontFromTTC tries to load the first font from a TrueType Collection
func tryLoadFirstFontFromTTC(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil { return false }

	// TTC: "ttcf" header, version, numFonts, offsets[numFonts], fontData...
	if len(data) >= 16 && string(data[:4]) == "ttcf" {
		numFonts := int(binary.BigEndian.Uint32(data[8:12]))
		if numFonts > 0 {
			// Calculate offset and size for first font
			startOff := int(binary.BigEndian.Uint32(data[12:16]))
			endOff := len(data)
			if numFonts > 1 {
				endOff = int(binary.BigEndian.Uint32(data[16:20]))
			}
			if startOff < len(data) && endOff <= len(data) && endOff > startOff {
				fontData := data[startOff:endOff]
				if f, err := freetype.ParseFont(fontData); err == nil {
					fontRegular, fontSmall = f, f
					log.Printf("[Font] ✅ Japanese font from TTC: %s", filepath.Base(path))
					return true
				}
				// Try with offset correction for SFNT header
				// Some TTC store offset from file start, some need 4-byte alignment
				for _, delta := range []int{0, -startOff} {
					if delta == 0 { continue }
					adjusted := make([]byte, endOff-startOff)
					copy(adjusted, data[startOff:endOff])
					if f, err := freetype.ParseFont(adjusted); err == nil {
						fontRegular, fontSmall = f, f
						return true
					}
				}
			}
		}
	}
	// Try direct parse (some TTC work as single font)
	if f, err := freetype.ParseFont(data); err == nil {
		fontRegular, fontSmall = f, f
		return true
	}
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
	// Use pre-loaded opentype font (handles TTC/OTF/TTF)
	if otFontData != nil {
		if drawWithOTFontData(img, x, y, text, col, size) {
			return
		}
	}
	// Fallback: tryDrawWithOpenType (searches all font paths)
	if tryDrawWithOpenType(img, x, y, text, col, size) {
		return
	}
	drawSimpleText(img, x, y, text, col, size)
}

func drawWithOTFontData(img *image.RGBA, x, y int, text string, col color.RGBA, size float64) bool {
	if otFontData == nil { return false }
	var sf *sfnt.Font
	var err error
	if coll, e := sfnt.ParseCollection(otFontData); e == nil {
		sf, err = coll.Font(0)
	} else {
		sf, err = sfnt.Parse(otFontData)
	}
	if err != nil || sf == nil { return false }
	face, err := opentype.NewFace(sf, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil { return false }
	defer face.Close()
	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	if ascent == 0 { ascent = int(size * 0.8) }
	d := &font.Drawer{
		Dst: img, Src: image.NewUniform(col), Face: face,
		Dot: fixed.P(x, y+ascent),
	}
	d.DrawString(text)
	return true
}

// tryDrawWithOpenType attempts to draw text using opentype package
