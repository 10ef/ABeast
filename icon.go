package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"runtime"
)

// MyGo fits the full image to 18pt high on macOS. Crop the logical canvas
// to y=3..27 so the dog itself is about 15.5pt high, rather than 12pt.
// Render at 2x for crisp edges; all states share the same canvas.
// Windows keeps a square canvas for the notification area's square slot.
const iconSize = 64

func trayIcon(state trayState) ([]byte, bool) {
	template := runtime.GOOS == "darwin" && (state == trayChecking || state == trayMonitoring || state == trayProtected)
	return renderTrayIcon(state, template), template
}

func renderTrayIcon(state trayState, template bool) []byte {
	ink := color.NRGBA{R: 217, G: 119, B: 87, A: 255}
	if template {
		ink = color.NRGBA{A: 255}
	}
	badge := ink
	switch state {
	case trayRecovering:
		badge = color.NRGBA{R: 106, G: 155, B: 204, A: 255}
	case trayWarning:
		badge = color.NRGBA{R: 190, G: 111, B: 9, A: 255}
	case trayPaused:
		badge = color.NRGBA{R: 213, G: 66, B: 60, A: 255}
	}
	return drawIcon(func(x, y float64) (color.NRGBA, bool) {
		if state != trayMonitoring {
			const badgeX, badgeY, badgeRadius = 26.5, 21.7, 4.6
			d := math.Hypot(x-badgeX, y-badgeY)
			if d <= badgeRadius {
				if badgeGlyph(state, (x-badgeX)*5.6/badgeRadius, (y-badgeY)*5.6/badgeRadius) {
					// Template images use alpha, not white paint, for negative space.
					return color.NRGBA{R: 255, G: 255, B: 255, A: 255}, !template
				}
				return badge, true
			}
			if d < badgeRadius+1.1 {
				return color.NRGBA{}, false
			}
		}
		return ink, insideIconPath(dogEars, x, y) ||
			math.Hypot(x-12.7, y-18.2) < 1.5 || math.Hypot(x-19.3, y-18.2) < 1.5 ||
			insideIconPath(dogNose, x, y)
	})
}

// badgeGlyph works in badge-local coordinates. Every state has a different
// shape as well as a color, so color alone never carries the status.
func badgeGlyph(state trayState, x, y float64) bool {
	switch state {
	case trayChecking:
		return math.Hypot(x+3, y) < 0.85 || math.Hypot(x, y) < 0.85 || math.Hypot(x-3, y) < 0.85
	case trayProtected:
		return iconStroke(x, y, -2.8, 0, -0.7, 2.1, 0.85) || iconStroke(x, y, -0.7, 2.1, 3, -2.2, 0.85)
	case trayWarning:
		return iconStroke(x, y, 0, -3, 0, 0.6, 0.9) || math.Hypot(x, y-3) < 0.95
	case trayPaused:
		return iconStroke(x, y, -1.8, -2.6, -1.8, 2.6, 0.85) || iconStroke(x, y, 1.8, -2.6, 1.8, 2.6, 0.85)
	case trayRecovering:
		// A clock: recovery is still being confirmed, not yet successful.
		return math.Abs(math.Hypot(x, y)-3.35) < 0.65 ||
			iconStroke(x, y, 0, -1.9, 0, 0.2, 0.65) || iconStroke(x, y, 0, 0.2, 1.7, 1.2, 0.65)
	}
	return false
}

func iconStroke(x, y, ax, ay, bx, by, r float64) bool {
	dx, dy := bx-ax, by-ay
	t := math.Max(0, math.Min(1, ((x-ax)*dx+(y-ay)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(x-ax-t*dx, y-ay-t*dy) <= r
}

type iconPoint struct{ x, y float64 }
type iconCurve struct{ c1, c2, end iconPoint }

// The connected forehead and folded ears follow the approved terracotta-dog
// logo. The face stays transparent; no white application tile is scaled
// into the menu bar. Curves are flattened once, then supersampled.
var dogEars = iconPath(iconPoint{16, 4.5}, []iconCurve{
	{iconPoint{21, 4.5}, iconPoint{23.5, 8.7}, iconPoint{26.5, 12.3}},
	{iconPoint{29.1, 15.2}, iconPoint{30.5, 17}, iconPoint{28.7, 20.5}},
	{iconPoint{27.2, 23.5}, iconPoint{24.7, 25.7}, iconPoint{22.9, 23.4}},
	{iconPoint{21.2, 21.3}, iconPoint{21.2, 18}, iconPoint{21.2, 13}},
	{iconPoint{21.2, 8.5}, iconPoint{19.5, 6.7}, iconPoint{16, 6.7}},
	{iconPoint{12.5, 6.7}, iconPoint{10.8, 8.5}, iconPoint{10.8, 13}},
	{iconPoint{10.8, 18}, iconPoint{10.8, 21.3}, iconPoint{9.1, 23.4}},
	{iconPoint{7.3, 25.7}, iconPoint{4.8, 23.5}, iconPoint{3.3, 20.5}},
	{iconPoint{1.5, 17}, iconPoint{2.9, 15.2}, iconPoint{5.5, 12.3}},
	{iconPoint{8.5, 8.7}, iconPoint{11, 4.5}, iconPoint{16, 4.5}},
})

var dogNose = iconPath(iconPoint{13.5, 22.5}, []iconCurve{
	{iconPoint{13.5, 21.3}, iconPoint{18.5, 21.3}, iconPoint{18.5, 22.5}},
	{iconPoint{18.5, 23.6}, iconPoint{16.9, 25.2}, iconPoint{16, 25.2}},
	{iconPoint{15.1, 25.2}, iconPoint{13.5, 23.6}, iconPoint{13.5, 22.5}},
})

func iconPath(start iconPoint, curves []iconCurve) []iconPoint {
	points := []iconPoint{start}
	for _, c := range curves {
		for i := 1; i <= 16; i++ {
			t := float64(i) / 16
			u := 1 - t
			points = append(points, iconPoint{
				u*u*u*start.x + 3*u*u*t*c.c1.x + 3*u*t*t*c.c2.x + t*t*t*c.end.x,
				u*u*u*start.y + 3*u*u*t*c.c1.y + 3*u*t*t*c.c2.y + t*t*t*c.end.y,
			})
		}
		start = c.end
	}
	return points
}

func insideIconPath(points []iconPoint, x, y float64) bool {
	inside := false
	prev := points[len(points)-1]
	for _, p := range points {
		if (p.y > y) != (prev.y > y) && x < (prev.x-p.x)*(y-p.y)/(prev.y-p.y)+p.x {
			inside = !inside
		}
		prev = p
	}
	return inside
}

// drawIcon fills each pixel from 4x4 samples of paint, for smooth edges.
func drawIcon(paint func(x, y float64) (color.NRGBA, bool)) []byte {
	height, originY := iconSize, 0.0
	if runtime.GOOS == "darwin" {
		height, originY = 48, 3
	}
	img := image.NewNRGBA(image.Rect(0, 0, iconSize, height))
	for py := 0; py < height; py++ {
		for px := 0; px < iconSize; px++ {
			var r, g, b, n float64
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					c, ok := paint((float64(px)+(float64(sx)+0.5)/4)/2, (float64(py)+(float64(sy)+0.5)/4)/2+originY)
					if ok {
						r, g, b, n = r+float64(c.R), g+float64(c.G), b+float64(c.B), n+1
					}
				}
			}
			if n > 0 {
				img.SetNRGBA(px, py, color.NRGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(b / n), A: uint8(255 * n / 16)})
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
