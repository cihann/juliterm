package server

import (
	"bytes"
	"encoding/json"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"strings"
	"unicode"
)

// Machine identity: with juli on several machines, each one needs its own
// name and icon, or Home Screen shortcuts on a phone or tablet all come out
// as an identical "J" with the same name. The name defaults to the short
// hostname; the icon is a colored square, its color derived from the name,
// with a short badge (the name's trailing number, else its first letters).

// DefaultName is the short hostname, e.g. "macstudio36".
func DefaultName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "juli"
	}
	h = strings.TrimSuffix(strings.ToLower(h), ".local")
	if i := strings.IndexByte(h, '.'); i > 0 {
		h = h[:i]
	}
	return h
}

// SetName sets the machine name shown in the page title, Home Screen name
// and icon.
func (s *Server) SetName(name string) {
	s.name = name
}

func (s *Server) displayName() string {
	if s.name == "" {
		return "juli"
	}
	return s.name
}

// badge is what the icon shows: a trailing number of up to three digits
// ("macstudio36" -> "36"), else the first two letters or digits, uppercased.
func badge(name string) string {
	r := []rune(name)
	end := len(r)
	start := end
	for start > 0 && end-start < 3 && unicode.IsDigit(r[start-1]) {
		start--
	}
	if start < end {
		return string(r[start:end])
	}
	var b []rune
	for _, c := range r {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b = append(b, unicode.ToUpper(c))
			if len(b) == 2 {
				break
			}
		}
	}
	if len(b) == 0 {
		return "J"
	}
	return string(b)
}

// iconPalette are dark-theme-friendly colors with good contrast for white text.
var iconPalette = []color.RGBA{
	{0x1f, 0x6f, 0xeb, 0xff}, // blue
	{0x23, 0x86, 0x36, 0xff}, // green
	{0x8b, 0x57, 0xd8, 0xff}, // purple
	{0xbf, 0x4b, 0x08, 0xff}, // orange
	{0xcf, 0x22, 0x2e, 0xff}, // red
	{0x0e, 0x7c, 0x86, 0xff}, // teal
	{0xbf, 0x39, 0x89, 0xff}, // pink
	{0x6e, 0x76, 0x81, 0xff}, // gray
}

func iconColor(name string) color.RGBA {
	h := fnv.New32a()
	h.Write([]byte(name))
	return iconPalette[h.Sum32()%uint32(len(iconPalette))]
}

func hexColor(c color.RGBA) string {
	const digits = "0123456789abcdef"
	b := []byte{'#', 0, 0, 0, 0, 0, 0}
	for i, v := range []uint8{c.R, c.G, c.B} {
		b[1+2*i] = digits[v>>4]
		b[2+2*i] = digits[v&0xf]
	}
	return string(b)
}

// 5x7 pixel glyphs for the badge: each row's low five bits, left to right.
var glyphs = map[rune][7]uint8{
	'0': {0x0e, 0x11, 0x13, 0x15, 0x19, 0x11, 0x0e}, '1': {0x04, 0x0c, 0x04, 0x04, 0x04, 0x04, 0x0e},
	'2': {0x0e, 0x11, 0x01, 0x02, 0x04, 0x08, 0x1f}, '3': {0x1f, 0x02, 0x04, 0x02, 0x01, 0x11, 0x0e},
	'4': {0x02, 0x06, 0x0a, 0x12, 0x1f, 0x02, 0x02}, '5': {0x1f, 0x10, 0x1e, 0x01, 0x01, 0x11, 0x0e},
	'6': {0x06, 0x08, 0x10, 0x1e, 0x11, 0x11, 0x0e}, '7': {0x1f, 0x01, 0x02, 0x04, 0x08, 0x08, 0x08},
	'8': {0x0e, 0x11, 0x11, 0x0e, 0x11, 0x11, 0x0e}, '9': {0x0e, 0x11, 0x11, 0x0f, 0x01, 0x02, 0x0c},
	'A': {0x0e, 0x11, 0x11, 0x1f, 0x11, 0x11, 0x11}, 'B': {0x1e, 0x11, 0x11, 0x1e, 0x11, 0x11, 0x1e},
	'C': {0x0e, 0x11, 0x10, 0x10, 0x10, 0x11, 0x0e}, 'D': {0x1c, 0x12, 0x11, 0x11, 0x11, 0x12, 0x1c},
	'E': {0x1f, 0x10, 0x10, 0x1e, 0x10, 0x10, 0x1f}, 'F': {0x1f, 0x10, 0x10, 0x1e, 0x10, 0x10, 0x10},
	'G': {0x0e, 0x11, 0x10, 0x17, 0x11, 0x11, 0x0f}, 'H': {0x11, 0x11, 0x11, 0x1f, 0x11, 0x11, 0x11},
	'I': {0x0e, 0x04, 0x04, 0x04, 0x04, 0x04, 0x0e}, 'J': {0x07, 0x02, 0x02, 0x02, 0x02, 0x12, 0x0c},
	'K': {0x11, 0x12, 0x14, 0x18, 0x14, 0x12, 0x11}, 'L': {0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x1f},
	'M': {0x11, 0x1b, 0x15, 0x15, 0x11, 0x11, 0x11}, 'N': {0x11, 0x11, 0x19, 0x15, 0x13, 0x11, 0x11},
	'O': {0x0e, 0x11, 0x11, 0x11, 0x11, 0x11, 0x0e}, 'P': {0x1e, 0x11, 0x11, 0x1e, 0x10, 0x10, 0x10},
	'Q': {0x0e, 0x11, 0x11, 0x11, 0x15, 0x12, 0x0d}, 'R': {0x1e, 0x11, 0x11, 0x1e, 0x14, 0x12, 0x11},
	'S': {0x0f, 0x10, 0x10, 0x0e, 0x01, 0x01, 0x1e}, 'T': {0x1f, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04},
	'U': {0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x0e}, 'V': {0x11, 0x11, 0x11, 0x11, 0x11, 0x0a, 0x04},
	'W': {0x11, 0x11, 0x11, 0x15, 0x15, 0x15, 0x0a}, 'X': {0x11, 0x11, 0x0a, 0x04, 0x0a, 0x11, 0x11},
	'Y': {0x11, 0x11, 0x11, 0x0a, 0x04, 0x04, 0x04}, 'Z': {0x1f, 0x01, 0x02, 0x04, 0x08, 0x10, 0x1f},
}

// renderIcon draws a size x size PNG: the name's color with its badge in
// white, centered. iOS rounds the corners of Home Screen icons itself.
func renderIcon(name string, size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := iconColor(name)
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = bg.R, bg.G, bg.B, 0xff
	}

	text := []rune(badge(name))
	// Glyph cells: 5 wide plus 1 of spacing between glyphs, 7 tall.
	cols := len(text)*6 - 1
	scale := size * 62 / 100 / cols
	if max := size * 45 / 100 / 7; scale > max {
		scale = max
	}
	if scale < 1 {
		scale = 1
	}
	x0 := (size - cols*scale) / 2
	y0 := (size - 7*scale) / 2
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	for gi, ch := range text {
		g, ok := glyphs[ch]
		if !ok {
			continue
		}
		for row := 0; row < 7; row++ {
			for col := 0; col < 5; col++ {
				if g[row]&(1<<(4-col)) == 0 {
					continue
				}
				px := x0 + (gi*6+col)*scale
				py := y0 + row*scale
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						img.SetRGBA(px+dx, py+dy, white)
					}
				}
			}
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// handleIdentity returns the machine's name, badge and color. Public: the
// login page uses it too, and it reveals nothing beyond the hostname.
func (s *Server) handleIdentity(w http.ResponseWriter, r *http.Request) {
	name := s.displayName()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"name":  name,
		"badge": badge(name),
		"color": hexColor(iconColor(name)),
	})
}

// handleManifest serves the web app manifest, so a Home Screen shortcut gets
// this machine's name and icon.
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	name := s.displayName()
	w.Header().Set("Content-Type", "application/manifest+json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name":       name + " · Juli",
		"short_name": name,
		// A per-machine id: with the same id everywhere, iPadOS treated the
		// Home Screen apps of different machines as one app, so adding the
		// second shortcut repointed the first at the second machine.
		"id":               "/?juli=" + name,
		"start_url":        "/",
		"display":          "standalone",
		"background_color": "#0d1117",
		"theme_color":      "#0d1117",
		"icons": []map[string]string{
			{"src": "/apple-touch-icon.png", "sizes": "180x180", "type": "image/png"},
			{"src": "/icon-512.png", "sizes": "512x512", "type": "image/png"},
		},
	})
}

func (s *Server) iconHandler(size int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(renderIcon(s.displayName(), size))
	}
}
