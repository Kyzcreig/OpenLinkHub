package lsh

import (
	"bytes"
	"testing"
)

// synthRamp builds a deterministic n-slot (n*3-byte) color region standing in
// for a real generateRgbEffect tick: a smooth, per-slot-distinct RGB ramp so
// every slot is non-zero and differs from its neighbours (catches zeroing,
// truncation, and stale/wrong-index copies).
func synthRamp(slots int) []byte {
	region := make([]byte, slots*3)
	for i := 0; i < slots; i++ {
		region[i*3+0] = byte(10 + i)            // R, ≥10 (never zero)
		region[i*3+1] = byte(200 - (i % 200))   // G
		region[i*3+2] = byte((i*7)%251 + 1)     // B, ≥1
	}
	return region
}

func synthRamp204() []byte { return synthRamp(fwColorSlots) }
func synthRamp170() []byte { return synthRamp(fwTailSlots) }

// TestBuildFirmwareFrame_Fill204Color is the PRIMARY (olh-fix-b204) contract:
// region-A (204-LED strip zone) + region-B (170-LED fan zone) BOTH carry real
// color → exactly 1122 bytes, both regions preserved verbatim, and crucially
// the fan zone is REAL color, NOT the legacy 0x5d 0xdd 0x32 green literal.
func TestBuildFirmwareFrame_Fill204Color(t *testing.T) {
	region := synthRamp204()
	tail := synthRamp170()
	frame, err := buildFirmwareFrame(region, tail, fillFill204, tailColor, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frame) != fwFrameBytes {
		t.Fatalf("frame len = %d, want %d", len(frame), fwFrameBytes)
	}
	// (a) region-A preserved verbatim.
	if !bytes.Equal(frame[:fwColorBytes], region) {
		t.Fatalf("region-A not preserved verbatim")
	}
	// (b) region-B (fan zone) preserved verbatim AND non-zero.
	gotTail := frame[fwColorBytes:]
	if allZero(gotTail) {
		t.Fatalf("fan zone is all zero — tailColor must carry real color")
	}
	if !bytes.Equal(gotTail, tail) {
		t.Fatalf("fan zone != commanded color region")
	}
	// (c) REGRESSION GUARD: the fan zone must NOT be the legacy green literal.
	legacy := bytes.Repeat(fwTailLiteral, fwTailSlots)
	if bytes.Equal(gotTail, legacy) {
		t.Fatalf("fan zone is the legacy 0x5d 0xdd 0x32 green literal — the #452 bug is back")
	}
	// (d) slots 170–203 of region-A carry the effect continuation (non-zero).
	ext := frame[fwTailSlots*3 : fwColorBytes]
	if allZero(ext) {
		t.Fatalf("region-A slots 170–203 are all zero — fill204 must carry continuation")
	}
	// wire length header must be 0x0464 (1124).
	if got := wireDataLen(frame); got != 0x0464 {
		t.Fatalf("wire data length = 0x%04x, want 0x0464", got)
	}
}

// TestBuildFirmwareFrame_SolidColorBothZones proves a SOLID commanded color
// (e.g. blue) lands in BOTH zones — the exact scenario that was failing live
// (black/red/blue all came out green on the fans).
func TestBuildFirmwareFrame_SolidColorBothZones(t *testing.T) {
	blue := []byte{0, 0, 255}
	regionA := bytes.Repeat(blue, fwColorSlots) // 612
	regionB := bytes.Repeat(blue, fwTailSlots)  // 510
	frame, err := buildFirmwareFrame(regionA, regionB, fillFill204, tailColor, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// every 3-byte slot across all 374 LEDs must equal blue.
	for off := 0; off+3 <= len(frame); off += 3 {
		if !bytes.Equal(frame[off:off+3], blue) {
			t.Fatalf("slot at byte %d = %v, want blue %v (fan zone must obey commanded color)",
				off, frame[off:off+3], blue)
		}
	}
}

// TestBuildFirmwareFrame_TailLiteralDiagnostic keeps the legacy green-literal
// behavior reachable (for A/B reproduction of the original bug). tailRegion is
// ignored in this mode.
func TestBuildFirmwareFrame_TailLiteralDiagnostic(t *testing.T) {
	region := synthRamp204()
	frame, err := buildFirmwareFrame(region, nil, fillFill204, tailLiteral, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frame) != fwFrameBytes {
		t.Fatalf("frame len = %d, want %d", len(frame), fwFrameBytes)
	}
	wantTail := bytes.Repeat(fwTailLiteral, fwTailSlots)
	if !bytes.Equal(frame[fwColorBytes:], wantTail) {
		t.Fatalf("tailLiteral: tail != 0x5d 0xdd 0x32 × %d", fwTailSlots)
	}
}

// TestBuildFirmwareFrame_TailZero is the DIAGNOSTIC (olh-fix-ztail): full 204
// fill + all-zero fan zone.
func TestBuildFirmwareFrame_TailZero(t *testing.T) {
	region := synthRamp204()
	frame, err := buildFirmwareFrame(region, nil, fillFill204, tailZero, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frame) != fwFrameBytes {
		t.Fatalf("frame len = %d, want %d", len(frame), fwFrameBytes)
	}
	if !allZero(frame[fwColorBytes:]) {
		t.Fatalf("tailZero: fan zone must be all zero")
	}
}

// TestBuildFirmwareFrame_BadSizes is contract (d): wrong-sized regions must
// ERROR (never emit a ≠1122 frame) — over-long/short region-A, and a missing/
// wrong-sized tail region in tailColor mode.
func TestBuildFirmwareFrame_BadSizes(t *testing.T) {
	goodTail := synthRamp170()
	cases := []struct {
		name string
		a    int
		tail []byte
		fm   frameFillMode
		tm   frameTailMode
	}{
		{"fill204 over-long region-A", fwFrameBytes, goodTail, fillFill204, tailColor},
		{"fill204 short region-A", fwTailBytes, goodTail, fillFill204, tailColor},
		{"fill204 empty region-A", 0, goodTail, fillFill204, tailColor},
		{"pad34 fed full 612", fwColorBytes, goodTail, fillReal170Pad34, tailColor},
		{"tailColor missing tail", fwColorBytes, nil, fillFill204, tailColor},
		{"tailColor short tail", fwColorBytes, make([]byte, 12), fillFill204, tailColor},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			frame, err := buildFirmwareFrame(make([]byte, c.a), c.tail, c.fm, c.tm, 99)
			if err == nil {
				t.Fatalf("expected error for %s, got frame len %d", c.name, len(frame))
			}
			if frame != nil {
				t.Fatalf("expected nil frame on error, got %d bytes", len(frame))
			}
		})
	}
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}
