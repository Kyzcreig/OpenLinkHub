package rgb

import (
	"math"
	"time"
)

// TealPinkHueCycle — OpenLinkHub-native port of Ace's ACE-MEDIA "Effect F" (teal->pink
// hue-arc SINE breathing wave). It sweeps a bounded HUE ARC at full saturation
// and sine ping-pongs it (teal->pink->teal) so the color travels along the
// SATURATED rim and NEVER crosses the desaturated center (no white flash that a
// straight RGB lerp like Colorshift produces between cyan and magenta).
//
// Effect-F features carried over:
//   - per-LED phase offset around each device's LED ring => the fan "breathes"
//     the arc like an ocean wave (each LED slightly ahead/behind its neighbour),
//   - PINK_BIAS so the breath lingers on the pink/purple side,
//   - a light per-ring blur (linear-light) to round LED-to-LED corners.
//
// Tunables mirror the Python daemon's TWEAK block. Arc endpoints derive from the
// configured StartColor/EndColor hues so the standard set-colors API configures
// the palette; the motion/bias/smoothing live here as named constants.
const (
	huePinkBias   = 1.0  // 1.0 = EVEN distribution across the teal->pink arc (full gradient, Ace-approved 2026-06-21). >1 lingers on pink (thin middle); <1 teal-heavy. GRADIENT-RICHNESS knob.
	hueWaveS      = 9.0  // seconds for one full teal->pink->teal breath
	hueDir        = -1.0 // wave travel direction around the ring (+1/-1)
	hueLedsPerFan = 18   // LEDs per breath (spatial WAVELENGTH, not a device LED count). The arc tiles every 18 LEDs so the teal->pink sine ping-pong FLOWS across fans AND long case strips (iCUE rainbow-flow bounded to the teal->pink arc -- the look Ace wants, inspired by the HA zsw teal_pink sine-wave model). Tune this for density: smaller=tighter waves, larger=longer/calmer. Do NOT set ring=device-length (stretches strips into one flat breath).
	hueSmooth     = 0.6  // per-ring blur strength (0=off .. ~0.8 soft); 0.6 glassy
	hueKernel     = 5    // 3=tight neighbours; 5=gaussian 2-each-side (glassier)
	// hueWaveMode = the TEMPORAL wave shape (how the breath moves teal<->pink over time):
	//   "sine"     = smooth glide; slows at the teal/pink endpoints, races through blue/purple
	//                (lingers on the ends, less dwell on the middle hues).
	//   "triangle" = constant speed; even dwell PER HUE (more visible blue/purple), tiny
	//                pause-and-reverse feel at the endpoints. Mirrors the HA zsw wave_mode knob.
	// A/B by editing this one constant + rebuild/redeploy (cheap). Default triangle (Ace 2026-06-21).
	hueWaveMode = "triangle"
	// hueLumaLift = brighten the intrinsically-DIM part of the arc (blue ~241deg has only ~7%%
	// of white luminance; teal/cyan ~50-78%) so blue/purple stop receding next to bright teal/pink.
	// 0 = off (true full-saturation arc); 1 = full lift to hueLumaTarget. Cost: lifted blue/purple
	// desaturate toward pastel (pure blue cannot brighten without adding R/G). GRADIENT-EVENNESS knob.
	hueLumaLift   = 0.0  // 0 = OFF (Ace reverted strong lift 2026-06-21; revisiting strips specifically)
	hueLumaTarget = 0.45 // target Rec.709 luminance floor (0..1) that dim hues are lifted toward
	// hueMidWidenAmt = blue/purple band widener (BOTH fans AND strips, Ace 2026-06-21). Remaps the wave
	// position toward the arc MIDDLE so more LEDs land in blue/purple = a physically wider, more visible
	// blue/purple band. Helps the diffused 5000T strips most (their narrow dim band was swallowed by bright
	// teal/pink neighbours) but applied everywhere for a consistent look. 0=off; ~1=widest. Pure hue remap
	// => NO desaturation/pastel cost (unlike luma-lift).
	hueMidWidenAmt = 1.0
)

func huePingpong(norm float64) float64 {
	return (math.Sin(2*math.Pi*norm-math.Pi/2) + 1) / 2
}

// hueTriangle is a constant-velocity 0->1->0 ping-pong (linear ramps). Even dwell per
// hue across the arc (more blue/purple) vs sine which lingers at the endpoints.
func hueTriangle(norm float64) float64 {
	if norm < 0.5 {
		return 2 * norm
	}
	return 2 * (1 - norm)
}

// hueWaveShape dispatches on hueWaveMode so the temporal wave is a live-tunable knob.
func hueWaveShape(norm float64) float64 {
	if hueWaveMode == "triangle" {
		return hueTriangle(norm)
	}
	return huePingpong(norm)
}

// hueMidWiden remaps wave position t toward the arc middle (0.5): t + (A/2pi)*sin(2pi*t).
// Flattens dwell near the middle so MORE LEDs land in blue/purple. Strip-only (diffusion fix).
func hueMidWiden(t, a float64) float64 {
	if a <= 0 {
		return t
	}
	v := t + (a/(2*math.Pi))*math.Sin(2*math.Pi*t)
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return v
}

// hueLumaCompensate brightens dim hues toward hueLumaTarget perceived luminance.
// Scales V (all channels up) only when the hue is below target, blended by hueLumaLift,
// clamping channels at 255 (clip slightly desaturates the deepest blue -- acceptable).
func hueLumaCompensate(cr, cg, cb int) (int, int, int) {
	if hueLumaLift <= 0 {
		return cr, cg, cb
	}
	l := (0.2126*float64(cr) + 0.7152*float64(cg) + 0.0722*float64(cb)) / 255.0
	if l <= 0 {
		return cr, cg, cb
	}
	want := hueLumaTarget / l
	if want < 1.0 {
		want = 1.0 // never DARKEN already-bright hues
	}
	scale := 1.0 + hueLumaLift*(want-1.0)
	nr := int(float64(cr)*scale + 0.5)
	ng := int(float64(cg)*scale + 0.5)
	nb := int(float64(cb)*scale + 0.5)
	return clamp8(nr), clamp8(ng), clamp8(nb)
}

func hueSkew(u, bias float64) float64 {
	if bias == 1.0 {
		return u
	}
	return math.Pow(u, 1.0/bias)
}

// TealPinkHueCycle renders the breathing hue-arc wave across this device's LEDs.
func (r *ActiveRGB) TealPinkHueCycle(startTime *time.Time) {
	buf := map[int][]byte{}

	if r.RgbModeSpeed == 0 {
		r.RgbModeSpeed = 1.0
	}
	// RgbModeSpeed (seconds) scales the configured breath period; default hueWaveS.
	waveS := hueWaveS * r.RgbModeSpeed / 4.0 // speed slider centres near 4 -> ~hueWaveS
	if waveS < 0.5 {
		waveS = 0.5
	}
	elapsed := time.Since(*startTime).Seconds()

	h0 := colorHue(r.RGBStartColor)
	h1 := colorHue(r.RGBEndColor)

	v := int(255 * r.RGBBrightness)
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}

	n := r.LightChannels
	if n <= 0 {
		n = 1
	}

	// ring = the spatial wavelength: how many LEDs one full teal->pink breath spans.
	// This is a FIXED visual wavelength (hueLedsPerFan), NOT the device's LED count.
	// Keeping it fixed makes every device -- an 18-LED LX fan, an 8-LED RX fan, a
	// 20-LED pump, and each 32-38-LED case-strip segment -- render the SAME rotating
	// teal->pink wave (the arc tiles repeatedly across long runs). Matches the
	// ACE-MEDIA OpenRGB daemon's LEDS_PER_FAN=8. (Setting ring=device-length instead
	// stretches the strips into one slow flat breath = boring; a too-long fixed ring
	// like 18 shows the whole 143deg arc at once on long strips = reads as rainbow.)
	ring := hueLedsPerFan

	// 1) per-LED arc color with ring phase offset
	cols := make([][3]int, n)
	for i := 0; i < n; i++ {
		ringPos := float64(i%ring) / float64(ring)
		norm := math.Mod(elapsed/waveS+hueDir*ringPos, 1.0)
		if norm < 0 {
			norm += 1.0
		}
		t := hueSkew(hueWaveShape(norm), huePinkBias)
		t = hueMidWiden(t, hueMidWidenAmt)
		hue := h0 + (h1-h0)*t
		cr, cg, cb := hueArcHSV(hue, 1.0, float64(v)/255.0)
		cr, cg, cb = hueLumaCompensate(cr, cg, cb)
		cols[i] = [3]int{cr, cg, cb}
	}

	// 2) per-ring blur (linear-light) to soften LED-to-LED steps
	if hueSmooth > 0 && n >= 3 {
		cols = hueBlurRings(cols, ring, hueSmooth, hueKernel)
	}

	for j := 0; j < n; j++ {
		cr := byte(cols[j][0])
		cg := byte(cols[j][1])
		cb := byte(cols[j][2])
		if len(r.Buffer) > 0 {
			r.Buffer[j] = cr
			r.Buffer[j+r.ColorOffset] = cg
			r.Buffer[j+(r.ColorOffset*2)] = cb
		} else {
			buf[j] = []byte{cr, cg, cb}
			if r.IsAIO && r.HasLCD {
				if j > 15 && j < 20 {
					buf[j] = []byte{0, 0, 0}
				}
			}
		}
	}

	r.Raw = buf
	if r.Inverted {
		r.Output = SetColorInverted(buf)
	} else {
		r.Output = SetColor(buf)
	}
}

// hueBlurRings blurs each LEDS_PER_FAN-sized ring segment independently (wrap)
// in linear light, preserving the per-fan look while rounding corners.
func hueBlurRings(cols [][3]int, ring int, strength float64, kernel int) [][3]int {
	out := make([][3]int, len(cols))
	copy(out, cols)
	for base := 0; base < len(cols); base += ring {
		end := base + ring
		if end > len(cols) {
			end = len(cols)
		}
		seg := cols[base:end]
		bl := hueBlurOne(seg, strength, kernel)
		copy(out[base:end], bl)
	}
	return out
}

func hueBlurOne(seg [][3]int, strength float64, kernel int) [][3]int {
	n := len(seg)
	if n < 3 {
		return seg
	}
	// to linear
	lin := make([][3]float64, n)
	for i, c := range seg {
		lin[i] = [3]float64{srgbToLin(c[0]), srgbToLin(c[1]), srgbToLin(c[2])}
	}
	var kern []float64
	var ksum float64
	var off int
	if kernel >= 5 && n >= 5 {
		kern = []float64{1, 4, 6, 4, 1}
		ksum = 16
		off = 2
	} else {
		kern = []float64{1, 2, 1}
		ksum = 4
		off = 1
	}
	out := make([][3]int, n)
	for i := 0; i < n; i++ {
		var acc [3]float64
		for k := -off; k <= off; k++ {
			w := kern[k+off]
			cc := lin[((i+k)%n+n)%n]
			acc[0] += w * cc[0]
			acc[1] += w * cc[1]
			acc[2] += w * cc[2]
		}
		var mixed [3]int
		for c := 0; c < 3; c++ {
			bl := acc[c] / ksum
			mx := (1.0-strength)*lin[i][c] + strength*bl
			mixed[c] = linToSrgb(mx)
		}
		out[i] = mixed
	}
	return out
}

func srgbToLin(v int) float64 {
	x := float64(v) / 255.0
	if x <= 0.04045 {
		return x / 12.92
	}
	return math.Pow((x+0.055)/1.055, 2.4)
}

func linToSrgb(x float64) int {
	if x < 0 {
		x = 0
	}
	if x > 1 {
		x = 1
	}
	var s float64
	if x <= 0.0031308 {
		s = 12.92 * x
	} else {
		s = 1.055*math.Pow(x, 1/2.4) - 0.055
	}
	r := int(s*255 + 0.5)
	if r < 0 {
		r = 0
	}
	if r > 255 {
		r = 255
	}
	return r
}

// hueArcHSV converts HSV (hue degrees, s/v 0..1) to 8-bit RGB using CORRECT
// float math. NOTE: the package HsvToRgb (rotator.go) is broken — its (h/60)*2
// uses Go integer division, which mangles the sector ramp (e.g. hue 181 -> magenta
// instead of cyan, hue 324 -> yellow-green instead of pink). Do NOT use it here.
func hueArcHSV(h, s, v float64) (int, int, int) {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	c := v * s
	hp := h / 60.0
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case hp < 1:
		r, g, b = c, x, 0
	case hp < 2:
		r, g, b = x, c, 0
	case hp < 3:
		r, g, b = 0, c, x
	case hp < 4:
		r, g, b = 0, x, c
	case hp < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	ri := int((r+m)*255 + 0.5)
	gi := int((g+m)*255 + 0.5)
	bi := int((b+m)*255 + 0.5)
	return clamp8(ri), clamp8(gi), clamp8(bi)
}

func clamp8(x int) int {
	if x < 0 {
		return 0
	}
	if x > 255 {
		return 255
	}
	return x
}

// colorHue returns the HSV hue (degrees, 0-360) of an RGB Color. Used to derive
// the hue-arc endpoints from the configured start/end colors.
func colorHue(c *Color) float64 {
	if c == nil {
		return 0
	}
	rr := c.Red / 255.0
	gg := c.Green / 255.0
	bb := c.Blue / 255.0
	max := math.Max(rr, math.Max(gg, bb))
	min := math.Min(rr, math.Min(gg, bb))
	d := max - min
	if d == 0 {
		return 0
	}
	var h float64
	switch max {
	case rr:
		h = math.Mod((gg-bb)/d, 6)
	case gg:
		h = (bb-rr)/d + 2
	default:
		h = (rr-gg)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h
}
