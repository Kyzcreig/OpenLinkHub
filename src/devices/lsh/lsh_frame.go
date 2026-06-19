package lsh

// #452 — fw 3.10.636 LINK System Hub full-frame builder.
//
// Phase 2 (iCUE capture + byte-diff) proved the firmware wants a 1122-byte
// color payload (= cmdWriteColor data length 0x0464/1124 incl. the 2-byte
// dataType prefix). Phase 3 LIVE-TEST (2026-06-18) proved its TRUE structure:
//
//	[ 612-byte region : 204 LED slots ][ 510-byte region : 170 LED slots ]
//
// BOTH halves are REAL per-LED color data — 374 LED slots total. The earlier
// "fixed 0x5d 0xdd 0x32 tail terminator" was a MISREAD: every iCUE reference
// capture we diffed happened to be a SOLID color (green), so the second region
// looked constant. Decoding proved it: in the green capture region-A = (18,44,10)
// and the "tail" = (93,221,50) — the SAME hue at a 5× brightness ratio, i.e. two
// LED zones (the 204-slot strip + the 170-slot fans) both rendering green. The
// old builder hardcoded 0x5d 0xdd 0x32 into the 170 fan slots, so the fans were
// nailed to green regardless of the commanded color (red/black/blue all → green
// on the wire). This builder fills BOTH regions with the commanded color/effect.
//
// buildFirmwareFrame is PURE (no I/O) so it is deterministically unit-testable;
// the call-site wiring methods below handle attemptID, pre-pad correlation
// logging, and loud-skip-on-error.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"OpenLinkHub/src/logger"
)

// Frame geometry (fw 3.10.636), all derived from the Phase-2 capture + Phase-3
// live test. The frame is one logical 374-LED color buffer split 204 | 170.
const (
	fwColorSlots = 204                        // region-A LED slots (the strip zone)
	fwColorBytes = fwColorSlots * 3           // 612
	fwTailSlots  = 170                        // region-B LED slots (the fan zone)
	fwTailBytes  = fwTailSlots * 3            // 510
	fwFrameBytes = fwColorBytes + fwTailBytes // 1122
	fwTotalSlots = fwColorSlots + fwTailSlots // 374
)

// fwTailLiteral is the legacy constant (green) the old builder wrongly emitted
// into the fan zone. KEPT ONLY for the tailLiteral diagnostic mode so we can
// reproduce the original "fans stuck green" behavior on demand for comparison.
var fwTailLiteral = []byte{0x5d, 0xdd, 0x32}

// frameFillMode selects how the 612-byte region-A is produced.
type frameFillMode int

const (
	// fillFill204: a full 204-LED effect (612 bytes). PRIMARY.
	fillFill204 frameFillMode = iota
	// fillReal170Pad34: 170 real LEDs (510 bytes) + zero-pad slots 170–203.
	// DIAGNOSTIC only (olh-fix-a170).
	fillReal170Pad34
)

// frameTailMode selects the 510-byte region-B (fan zone).
type frameTailMode int

const (
	// tailColor: region-B carries REAL per-LED color (the commanded color/effect
	// continued across the fan zone). PRIMARY — this is what makes the fans obey.
	tailColor frameTailMode = iota
	// tailLiteral: the legacy 0x5d 0xdd 0x32 green repeat. DIAGNOSTIC ONLY
	// (reproduces the "fans stuck green" bug for A/B comparison).
	tailLiteral
	// tailZero: all-zero region-B (DIAGNOSTIC, olh-fix-ztail).
	tailZero
)

// activeFillMode / activeTailMode are the per-binary build selectors.
//
//	olh-fix-b204  : fillFill204      + tailColor    (PRIMARY — fans obey)
//	olh-fix-a170  : fillReal170Pad34 + tailColor    (diagnostic)
//	olh-fix-ztail : fillFill204      + tailZero     (diagnostic)
//	olh-base      : built from pristine lsh.go.orig (helper not called)
var (
	activeFillMode = fillFill204
	activeTailMode = tailColor
)

// buildFirmwareFrame assembles the full 1122-byte #452 color frame from region-A
// (612 bytes) and region-B / tail (510 bytes). Pure; never emits a malformed
// frame — any size mismatch returns an error so the caller can log LOUDLY and
// skip (anti-fabrication: "still wrong color" must never be confused with "never
// emitted a valid frame"). tailRegion is REQUIRED (len 510) for tailColor; it is
// ignored for tailLiteral / tailZero.
func buildFirmwareFrame(colorRegion, tailRegion []byte, fm frameFillMode, tm frameTailMode, attemptID uint64) ([]byte, error) {
	region := make([]byte, fwColorBytes)
	switch fm {
	case fillFill204:
		if len(colorRegion) != fwColorBytes {
			return nil, fmt.Errorf("#452 buildFirmwareFrame fill204: color region %d != %d bytes (attempt %d)",
				len(colorRegion), fwColorBytes, attemptID)
		}
		copy(region, colorRegion)
	case fillReal170Pad34:
		if len(colorRegion) != fwTailSlots*3 {
			return nil, fmt.Errorf("#452 buildFirmwareFrame real170pad34: color region %d != %d bytes (attempt %d)",
				len(colorRegion), fwTailSlots*3, attemptID)
		}
		copy(region, colorRegion) // slots 170–203 remain 0x00
	default:
		return nil, fmt.Errorf("#452 buildFirmwareFrame: unknown fillMode %d (attempt %d)", fm, attemptID)
	}

	frame := make([]byte, 0, fwFrameBytes)
	frame = append(frame, region...)
	switch tm {
	case tailColor:
		if len(tailRegion) != fwTailBytes {
			return nil, fmt.Errorf("#452 buildFirmwareFrame tailColor: tail region %d != %d bytes (attempt %d)",
				len(tailRegion), fwTailBytes, attemptID)
		}
		frame = append(frame, tailRegion...)
	case tailLiteral:
		frame = append(frame, bytes.Repeat(fwTailLiteral, fwTailSlots)...)
	case tailZero:
		frame = append(frame, make([]byte, fwTailBytes)...)
	default:
		return nil, fmt.Errorf("#452 buildFirmwareFrame: unknown tailMode %d (attempt %d)", tm, attemptID)
	}

	if len(frame) != fwFrameBytes {
		return nil, fmt.Errorf("#452 buildFirmwareFrame: assembled %d != %d bytes (attempt %d)",
			len(frame), fwFrameBytes, attemptID)
	}
	return frame, nil
}

// wireDataLen returns the 2-byte length-header value writeColor writes:
// len(data)+2 (the +2 = the dataTypeSetColor prefix). For the 1122-byte frame
// this MUST equal 0x0464 (1124) to match the iCUE capture. For the unit test.
func wireDataLen(data []byte) uint16 {
	return uint16(len(data) + len(dataTypeSetColor))
}

// frame452WorkDir is where pre-pad color-region hex is logged for frame↔capture
// correlation during the eyes-on / usbmon window.
const frame452WorkDir = "/home/ace/olh-452-work"

// frame452Attempt is a monotonic per-frame counter; embedded in the pre-pad log
// so a captured wire frame binds to the exact color region that produced it.
var frame452Attempt atomic.Uint64

// continueColorRegion returns exactly want bytes: copies src, and if src is short
// continues the LAST 3-byte color into the remaining slots (natural continuation
// of a solid color), or zero-fills when there is no color to continue. Longer
// src is truncated. Used by the static path to reach 612/510 deterministically.
func continueColorRegion(src []byte, want int) []byte {
	out := make([]byte, want)
	n := copy(out, src)
	if n >= want || n < 3 {
		return out
	}
	last := out[n-3 : n]
	for off := n; off+3 <= want; off += 3 {
		copy(out[off:off+3], last)
	}
	return out
}

// regionEffect generates an n-LED color region as a continuous virtual strip:
// the first REAL-LED channel's active RGB profile + the shared effect time base.
// keys[0] is typically the LINK adapter (channel 1, 0 LEDs, profile "static");
// we pick the first channel with LedChannels>0 so the real flowing effect drives
// the buffer. A wrong length is caught downstream by buildFirmwareFrame.
func (d *Device) regionEffect(keys []int, startTime *time.Time, slots uint8) []byte {
	if len(keys) == 0 {
		return make([]byte, int(slots)*3)
	}
	k := keys[0]
	for _, c := range keys {
		if d.Devices[c].LedChannels > 0 && !d.Devices[c].IsLinkAdapter {
			k = c
			break
		}
	}
	prof := d.Devices[k].RGB
	return d.generateRgbEffect(k, slots, startTime, prof, 0)
}

// region204 is the region-A (strip-zone) generator: 204 slots of the active
// effect. Kept as a thin wrapper for readability at the call sites.
func (d *Device) region204(keys []int, startTime *time.Time) []byte {
	return d.regionEffect(keys, startTime, fwColorSlots)
}

// region170 is the region-B (fan-zone) generator: 170 slots of the SAME active
// effect, phase-aligned via the shared startTime so the fans flow with the
// strip instead of being nailed to a constant. This is the #452 fix.
func (d *Device) region170(keys []int, startTime *time.Time) []byte {
	return d.regionEffect(keys, startTime, fwTailSlots)
}

// logPrepad writes the pre-pad color region (hex) to a single overwritten file,
// throttled to ~once/50 frames, tagged with attemptID, for frame↔capture
// correlation. Best-effort.
func logPrepad(region []byte, aid uint64) {
	if aid%50 != 1 {
		return
	}
	content := fmt.Sprintf("attempt=%d len=%d\n%s\n", aid, len(region), hex.EncodeToString(region))
	_ = os.WriteFile(frame452WorkDir+"/prepad-latest.hex", []byte(content), 0644)
}

// buildTailRegion produces the 510-byte region-B for the active tail mode. For
// tailColor it returns real color; for the diagnostics it returns a placeholder
// (buildFirmwareFrame substitutes the literal/zero, but we keep the length right).
func (d *Device) buildTailRegionLive(keys []int, startTime *time.Time) []byte {
	if activeTailMode == tailColor {
		return d.region170(keys, startTime)
	}
	return make([]byte, fwTailBytes)
}

// writeColorLive452 is the live-RGB-loop write path for #452: assemble the full
// 1122-byte frame (BOTH zones carry the live effect), log LOUDLY + SKIP on any
// build error (never emit ≠1122).
func (d *Device) writeColorLive452(buff []byte, keys []int, startTime *time.Time) {
	var region []byte
	switch activeFillMode {
	case fillFill204:
		region = d.region204(keys, startTime)
	default: // fillReal170Pad34
		region = continueColorRegion(buff, fwTailSlots*3) // 510 bytes
	}
	tail := d.buildTailRegionLive(keys, startTime)
	aid := frame452Attempt.Add(1)
	frame, err := buildFirmwareFrame(region, tail, activeFillMode, activeTailMode, aid)
	if err != nil {
		logger.Log(logger.Fields{"error": err, "serial": d.Serial, "attempt": aid}).
			Error("#452 live frame build failed; skipping write")
		return
	}
	logPrepad(region, aid)
	d.writeColor(frame)
}

// writeColorStatic452 is the static (write-once) path for #452: extend the solid
// static color across BOTH zones so the fans show the commanded color, assemble
// the full frame, same loud-skip-on-error discipline.
func (d *Device) writeColorStatic452(buffer []byte) {
	var region []byte
	switch activeFillMode {
	case fillFill204:
		region = continueColorRegion(buffer, fwColorBytes) // 612
	default: // fillReal170Pad34
		region = continueColorRegion(buffer, fwTailSlots*3) // 510
	}
	tail := continueColorRegion(buffer, fwTailBytes) // 510 — commanded color in the fan zone
	aid := frame452Attempt.Add(1)
	frame, err := buildFirmwareFrame(region, tail, activeFillMode, activeTailMode, aid)
	if err != nil {
		logger.Log(logger.Fields{"error": err, "serial": d.Serial, "attempt": aid}).
			Error("#452 static frame build failed; skipping write")
		return
	}
	logPrepad(region, aid)
	d.writeColor(frame)
}
