package main

import (
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
	for _, p := range platformLoadFontPaths() {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		sf, err := parseFont(data, p)
		if err != nil || sf == nil {
			continue
		}
		face, err := opentype.NewFace(sf, &opentype.FaceOptions{Size: 14, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			continue
		}
		face.Close()

		if tf, err := freetype.ParseFont(data); err == nil {
			fontRegular, fontSmall = tf, tf
		}
		otFont = sf
		otFontData = data
		infoLog("Loaded font: %s", filepath.Base(p))
		return
	}
	warnLog("No usable font found. Using fallback rectangles.")
}

func parseFont(data []byte, path string) (*sfnt.Font, error) {
	if strings.HasSuffix(strings.ToLower(path), ".ttc") {
		coll, err := sfnt.ParseCollection(data)
		if err != nil {
			return nil, err
		}
		return coll.Font(0)
	}
	return sfnt.Parse(data)
}

func measureText(text string, size float64) int {
	if fontRegular == nil {
		return len(text) * int(size*0.6)
	}
	face := truetype.NewFace(fontRegular, &truetype.Options{Size: size, DPI: 72})
	defer face.Close()
	return font.MeasureString(face, text).Ceil()
}

func drawText(img *image.RGBA, x, y int, text string, col color.RGBA, size float64) {
	if otFontData != nil && drawWithFont(img, x, y, text, col, size, otFontData) {
		return
	}
	for _, p := range platformLoadFontPaths() {
		if data, err := os.ReadFile(p); err == nil {
			if drawWithFont(img, x, y, text, col, size, data) {
				return
			}
		}
	}
	drawSimpleText(img, x, y, text, col, size)
}

func drawWithFont(img *image.RGBA, x, y int, text string, col color.RGBA, size float64, data []byte) bool {
	sf, err := parseFont(data, "")
	if err != nil || sf == nil {
		return false
	}
	face, err := opentype.NewFace(sf, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return false
	}
	defer face.Close()

	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	if ascent == 0 {
		ascent = int(size * 0.8)
	}
	d := &font.Drawer{
		Dst: img, Src: image.NewUniform(col), Face: face,
		Dot: fixed.P(x, y+ascent),
	}
	d.DrawString(text)
	return true
}