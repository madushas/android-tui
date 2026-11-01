package core

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
)

// RenderHalfBlocks draws a PNG with truecolor half-block characters (works in any modern terminal).
// The result fits within cols × rows terminal cells and keeps the aspect ratio.
func RenderHalfBlocks(data []byte, cols, rows int) ([]string, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	iw, ih := b.Dx(), b.Dy()
	if iw == 0 || ih == 0 || cols < 2 || rows < 1 {
		return nil, fmt.Errorf("nothing to draw")
	}
	sx, sy := float64(cols)/float64(iw), float64(rows*2)/float64(ih)
	sc := sx
	if sy < sc {
		sc = sy
	}
	tw, th := max(1, int(float64(iw)*sc)), max(2, int(float64(ih)*sc))
	th -= th % 2
	px := func(x, y int) (uint8, uint8, uint8) {
		// box-average a few samples across the source footprint of this target pixel
		x0, x1 := x*iw/tw, max(x*iw/tw+1, (x+1)*iw/tw)
		y0, y1 := y*ih/th, max(y*ih/th+1, (y+1)*ih/th)
		var r, g, bl, n uint32
		stepx, stepy := max(1, (x1-x0)/3), max(1, (y1-y0)/3)
		for yy := y0; yy < y1 && yy < ih; yy += stepy {
			for xx := x0; xx < x1 && xx < iw; xx += stepx {
				cr, cg, cb, _ := img.At(b.Min.X+xx, b.Min.Y+yy).RGBA()
				r, g, bl, n = r+cr>>8, g+cg>>8, bl+cb>>8, n+1
			}
		}
		if n == 0 {
			return 0, 0, 0
		}
		return uint8(r / n), uint8(g / n), uint8(bl / n)
	}
	var out []string
	for y := 0; y+1 < th; y += 2 {
		var sb strings.Builder
		for x := 0; x < tw; x++ {
			r1, g1, b1 := px(x, y)
			r2, g2, b2 := px(x, y+1)
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", r1, g1, b1, r2, g2, b2)
		}
		sb.WriteString("\x1b[0m")
		out = append(out, sb.String())
	}
	return out, nil
}

// ImageSize returns the PNG's pixel dimensions.
func ImageSize(data []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// ImageProtocol picks the best way to show a full-resolution image in this terminal.
func ImageProtocol() string {
	term, prog := os.Getenv("TERM"), os.Getenv("TERM_PROGRAM")
	switch {
	case os.Getenv("KITTY_WINDOW_ID") != "" || strings.Contains(term, "kitty") || strings.Contains(term, "ghostty") || prog == "ghostty" || prog == "WezTerm":
		return "kitty"
	case Which("chafa"):
		return "chafa"
	case Which("img2sixel"):
		return "sixel"
	}
	return "blocks"
}

// KittySeq builds the Kitty graphics-protocol escape sequence for a PNG (direct transmission, chunked).
func KittySeq(data []byte, cols, rows int) string {
	enc := base64.StdEncoding.EncodeToString(data)
	var sb strings.Builder
	for i := 0; i < len(enc); i += 4096 {
		end := min(i+4096, len(enc))
		more := 1
		if end == len(enc) {
			more = 0
		}
		if i == 0 {
			fmt.Fprintf(&sb, "\x1b_Ga=T,f=100,c=%d,r=%d,m=%d;%s\x1b\\", cols, rows, more, enc[i:end])
		} else {
			fmt.Fprintf(&sb, "\x1b_Gm=%d;%s\x1b\\", more, enc[i:end])
		}
	}
	return sb.String()
}
