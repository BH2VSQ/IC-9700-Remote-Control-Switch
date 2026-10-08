package civ

import (
	"bytes"
	"math"
	"testing"
)

func TestBuildFrame(t *testing.T) {
	got := BuildFrame(0x1A, 0x05, 0x01, 0x05, 0x00)
	want := []byte{0xFE, 0xFE, 0xA2, 0xE0, 0x1A, 0x05, 0x01, 0x05, 0x00, 0xFD}
	if !bytes.Equal(got, want) {
		t.Fatalf("frame mismatch: % X != % X", got, want)
	}
}

func TestExtractFrames(t *testing.T) {
	data := []byte{0x00, 0xFE, 0xFE, 0xE0, 0xA2, 0xFB, 0xFD, 0x11, 0xFE, 0xFE, 0xE0, 0xA2, 0x1A, 0xFD}
	frames := ExtractFrames(data)
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
	if !isACK(frames[0]) {
		t.Fatalf("first frame is not ACK: % X", frames[0])
	}
}

func TestParseReadByteIC9700Settings(t *testing.T) {
	tests := []struct {
		name  string
		frame []byte
		cmd   byte
		sub   byte
		want  byte
	}{
		{
			name:  "DATA OFF MOD LAN",
			frame: []byte{0xFE, 0xFE, 0xE0, 0xA2, 0x1A, 0x05, 0x01, 0x15, 0x05, 0xFD},
			cmd:   0x1A,
			sub:   0x15,
			want:  0x05,
		},
		{
			name:  "DATA MOD LAN",
			frame: []byte{0xFE, 0xFE, 0xE0, 0xA2, 0x1A, 0x05, 0x01, 0x16, 0x05, 0xFD},
			cmd:   0x1A,
			sub:   0x16,
			want:  0x05,
		},
		{
			name:  "USB IF",
			frame: []byte{0xFE, 0xFE, 0xE0, 0xA2, 0x1A, 0x05, 0x01, 0x05, 0x01, 0xFD},
			cmd:   0x1A,
			sub:   0x05,
			want:  0x01,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseReadByte(tc.frame, tc.cmd, tc.sub, tc.name)
			if err != nil {
				t.Fatalf("parseReadByte failed: %v", err)
			}
			if got != tc.want {
				t.Fatalf("value mismatch: got %02X want %02X", got, tc.want)
			}
		})
	}
}

func TestSatelliteModeCIFrame(t *testing.T) {
	read := BuildFrame(0x16, 0x5A)
	wantRead := []byte{0xFE, 0xFE, 0xA2, 0xE0, 0x16, 0x5A, 0xFD}
	if !bytes.Equal(read, wantRead) {
		t.Fatalf("satellite read frame mismatch: % X != % X", read, wantRead)
	}

	write := BuildFrame(0x16, 0x5A, 0x01)
	wantWrite := []byte{0xFE, 0xFE, 0xA2, 0xE0, 0x16, 0x5A, 0x01, 0xFD}
	if !bytes.Equal(write, wantWrite) {
		t.Fatalf("satellite write frame mismatch: % X != % X", write, wantWrite)
	}
}

func TestParseSatelliteModeByte(t *testing.T) {
	frame := []byte{0xFE, 0xFE, 0xE0, 0xA2, 0x16, 0x5A, 0x01, 0xFD}
	got, err := parseCommandDataByte(frame, 0x16, 0x5A, "Satellite Mode")
	if err != nil {
		t.Fatalf("parseCommandDataByte failed: %v", err)
	}
	if got != 0x01 {
		t.Fatalf("satellite mode mismatch: got %02X want 01", got)
	}
}

func TestParseSatelliteModeOffByte(t *testing.T) {
	frame := []byte{0xFE, 0xFE, 0xE0, 0xA2, 0x16, 0x5A, 0x00, 0xFD}
	got, err := parseCommandDataByte(frame, 0x16, 0x5A, "Satellite Mode")
	if err != nil {
		t.Fatalf("parseCommandDataByte failed: %v", err)
	}
	if got != 0x00 {
		t.Fatalf("satellite mode mismatch: got %02X want 00", got)
	}
}

func TestFrequencyBCDEncoding(t *testing.T) {
	tests := []struct {
		hz   uint64
		want []byte
	}{
		{145875000, []byte{0x00, 0x50, 0x87, 0x45, 0x01}},
		{433595000, []byte{0x00, 0x50, 0x59, 0x33, 0x04}},
		{1240000000, []byte{0x00, 0x00, 0x00, 0x40, 0x12}},
	}
	for _, tc := range tests {
		got, err := encodeFrequency(tc.hz)
		if err != nil {
			t.Fatalf("encodeFrequency(%d): %v", tc.hz, err)
		}
		if !bytes.Equal(got, tc.want) {
			t.Fatalf("frequency %d encoded as % X, want % X", tc.hz, got, tc.want)
		}
		frame := append([]byte{0xFE, 0xFE, 0xE0, 0xA2, 0x03}, got...)
		frame = append(frame, 0xFD)
		decoded, err := parseFrequency(frame, "test")
		if err != nil {
			t.Fatalf("parseFrequency(%d): %v", tc.hz, err)
		}
		if decoded != tc.hz {
			t.Fatalf("frequency round trip mismatch: got %d want %d", decoded, tc.hz)
		}
	}
}

func TestParseRadioMode(t *testing.T) {
	for code, want := range map[byte]string{
		0x00: "LSB",
		0x01: "USB",
		0x02: "AM",
		0x03: "CW",
		0x04: "RTTY",
		0x05: "FM",
		0x07: "CW-R",
		0x08: "RTTY-R",
		0x17: "DV",
		0x22: "DD",
	} {
		mode, ok := ParseRadioMode(code)
		if !ok || mode.String() != want {
			t.Fatalf("mode %02X parsed as %q (%v), want %q", code, mode.String(), ok, want)
		}
	}
}

func TestBCD255EncodingAndParsing(t *testing.T) {
	tests := []struct {
		name string
		raw  int
		bcd  []byte
	}{
		{name: "zero", raw: 0, bcd: []byte{0x00, 0x00}},
		{name: "one hundred twenty eight", raw: 128, bcd: []byte{0x01, 0x28}},
		{name: "max", raw: 255, bcd: []byte{0x02, 0x55}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := encodeBCD255(tc.raw)
			if !bytes.Equal(got, tc.bcd) {
				t.Fatalf("encodeBCD255(%d) = % X, want % X", tc.raw, got, tc.bcd)
			}
			frame := append([]byte{0xFE, 0xFE, 0xE0, 0xA2, 0x14, 0x0A}, tc.bcd...)
			frame = append(frame, 0xFD)
			decoded, err := parseBCD255(frame, 0x14, 0x0A, tc.name)
			if err != nil {
				t.Fatalf("parseBCD255(%d) failed: %v", tc.raw, err)
			}
			if decoded != tc.raw {
				t.Fatalf("parseBCD255(%d) = %d", tc.raw, decoded)
			}
		})
	}
}

func TestRadioAssistScaling(t *testing.T) {
	if got := clampInt(0, 0, 10); got != 0 {
		t.Fatalf("clamp lower bound = %d", got)
	}
	if got := clampInt(255, 0, 100); got != 100 {
		t.Fatalf("clamp upper bound = %d", got)
	}
	for _, tc := range []struct {
		level int
		want  []byte
	}{
		{0, []byte{0x00, 0x00}},
		{5, []byte{0x01, 0x28}},
		{10, []byte{0x02, 0x55}},
	} {
		raw := int(math.Round(float64(tc.level) * 255.0 / 10.0))
		if got := encodeBCD255(raw); !bytes.Equal(got, tc.want) {
			t.Fatalf("COMP level %d encoded as % X, want % X", tc.level, got, tc.want)
		}
	}
}
