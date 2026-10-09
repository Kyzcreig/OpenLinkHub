package lsh

import (
	"encoding/hex"
	"strings"
	"testing"
)

// Captured on ACE-AI 2026-10-08 after the hub stopped answering the device-table query: the firmware
// returned a packet with data type 03 0a instead of 21 00. 512 bytes; everything after byte 7 is zero.
const badDeviceTableHeader = "00 00 08 03 03 0a 7c 02"

// Captured on the same hub on 2026-10-07 (healthy): 22 channels, 12 devices.
const goodDeviceTableReply = "" +
	"00 00 08 00 21 00 16 00 00 05 02 00 00 05 1a 30 31 30 30 30 34 38 43 38 32 30 33 36 45 38 36 45 36 30 30 30 30 37 44 45 39 00 00 11 02 00 00 05 " +
	"1a 30 31 30 30 32 46 32 45 38 32 30 33 36 42 39 37 37 39 30 30 30 30 33 31 46 43 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 " +
	"00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 " +
	"00 00 00 00 00 00 00 00 00 00 00 00 00 02 00 00 00 05 1a 30 31 30 30 31 33 37 33 30 32 30 33 36 42 33 43 30 36 30 30 30 31 36 30 34 36 00 00 02 " +
	"00 00 00 05 1a 30 31 30 30 31 44 31 37 31 32 30 33 36 42 34 43 36 34 30 30 30 30 35 35 30 35 00 00 02 00 00 00 05 1a 30 31 30 30 33 42 39 34 31 " +
	"32 30 33 36 42 33 30 46 44 30 30 30 30 30 32 37 32 00 00 02 00 00 00 05 1a 30 31 30 30 30 34 35 41 32 32 30 33 37 32 31 32 30 35 30 30 30 30 39 " +
	"38 39 41 00 00 02 00 00 00 05 1a 30 31 30 30 31 33 39 30 36 32 30 33 37 31 45 39 37 33 30 30 30 30 34 39 41 42 00 00 02 00 00 00 05 1a 30 31 30 " +
	"30 31 33 39 30 36 32 30 33 37 31 45 39 37 33 30 30 30 30 33 43 34 30 00 00 0f 00 00 00 05 1a 30 31 30 30 30 38 30 39 33 32 30 33 36 44 31 30 36 " +
	"38 30 30 30 30 41 32 34 45 00 00 0f 00 00 00 05 1a 30 31 30 30 31 38 46 30 39 32 30 33 36 45 44 43 37 32 30 30 30 30 45 45 31 37 00 00 0f 00 00 " +
	"00 05 1a 30 31 30 30 31 37 42 44 39 32 30 33 36 45 36 30 32 38 30 30 30 30 33 35 36 41 00 00 02 00 00 00 05 1a 30 31 30 30 30 34 35 41 32 32 30 " +
	"33 37 32 31 32 30 35 30 30 30 30 41 38 34 36 "

func decodeReply(t *testing.T, s string, size int) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	if err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	if len(b) < size {
		b = append(b, make([]byte, size-len(b))...)
	}
	return b
}

func TestParseDeviceTable_CapturedMalformedReply(t *testing.T) {
	reply := decodeReply(t, badDeviceTableHeader, bufferSize)
	if len(reply) != 512 {
		t.Fatalf("fixture length = %d, want 512", len(reply))
	}
	entries, err := parseDeviceTable(reply)
	if err == nil {
		t.Fatalf("expected an error for a wrong data type, got %d entries", len(entries))
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(entries))
	}
}

func TestParseDeviceTable_ValidReply(t *testing.T) {
	reply := decodeReply(t, goodDeviceTableReply, 1020)
	entries, err := parseDeviceTable(reply)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 12 {
		t.Fatalf("entries = %d, want 12", len(entries))
	}
	wantChannels := []int{1, 2, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22}
	for k, e := range entries {
		if e.channel != wantChannels[k] {
			t.Errorf("entry %d channel = %d, want %d", k, e.channel, wantChannels[k])
		}
		if len(e.header) != 8 || len(e.deviceId) != 26 {
			t.Errorf("entry %d header/id lengths = %d/%d, want 8/26", k, len(e.header), len(e.deviceId))
		}
	}
	if got := string(entries[1].deviceId); got != "01002F2E82036B9779000031FC" {
		t.Errorf("TITAN 360 device id = %q", got)
	}
	if entries[1].header[2] != 17 || entries[0].header[2] != 5 {
		t.Errorf("types = %d/%d, want 5 (adapter) / 17 (TITAN)", entries[0].header[2], entries[1].header[2])
	}
}

// A correctly typed reply that lost its second 508-byte chunk must be rejected, not half-parsed.
func TestParseDeviceTable_TruncatedValidReply(t *testing.T) {
	full := decodeReply(t, goodDeviceTableReply, 1020)
	for _, n := range []int{400, 300, 100, 15, 8} { // 22-ch table = 488 data bytes, so 512 is NOT truncated
		if _, err := parseDeviceTable(full[:n]); err == nil {
			t.Errorf("len %d: expected error for truncated table", n)
		}
	}
}

// Channel count larger than the buffer can hold (the original panic: 0x7c channels in 505 data bytes).
func TestParseDeviceTable_ChannelCountOverrunsBuffer(t *testing.T) {
	reply := make([]byte, bufferSize)
	copy(reply, []byte{0x00, 0x00, 0x08, 0x00, 0x21, 0x00, 0x7c})
	if _, err := parseDeviceTable(reply); err == nil {
		t.Fatal("expected error when channel table runs past the reply")
	}
}

func TestParseDeviceTable_ShortReply(t *testing.T) {
	for _, r := range [][]byte{nil, {}, {0x00, 0x00, 0x08, 0x00, 0x21, 0x00, 0x01}} {
		if _, err := parseDeviceTable(r); err == nil {
			t.Errorf("len %d: expected error", len(r))
		}
	}
}
