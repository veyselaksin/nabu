package tts

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"
)

func TestEncodeWAV(t *testing.T) {
	var buf bytes.Buffer
	samples := []float32{0, 1, -1, 0.5, -0.5, 2, -2, float32(math.NaN())}
	if err := EncodeWAV(&buf, samples, 16000); err != nil {
		t.Fatal(err)
	}
	want := "52494646" + "34000000" + "57415645" + // RIFF, size 36+16, WAVE
		"666d7420" + "10000000" + "0100" + "0100" + // fmt , 16, PCM, mono
		"803e0000" + "007d0000" + "0200" + "1000" + // 16000 Hz, 32000 B/s, align 2, 16 bit
		"64617461" + "10000000" + // data, 16 bytes
		"0000" + "ff7f" + "0180" + "0040" + "00c0" + "ff7f" + "0180" + "0000"
	if got := hex.EncodeToString(buf.Bytes()); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestEncodeWAVEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeWAV(&buf, nil, 22050); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if len(b) != 44 || binary.LittleEndian.Uint32(b[40:]) != 0 || binary.LittleEndian.Uint32(b[4:]) != 36 {
		t.Errorf("unexpected empty WAV header %x", b)
	}
	if err := EncodeWAV(&buf, nil, 0); err == nil {
		t.Error("expected error for sample rate 0")
	}
}

func TestPCM16Rounding(t *testing.T) {
	for in, want := range map[float32]int16{0.25: 8192, -0.25: -8192, 1e-6: 0, 0.99999: 32767} {
		if got := pcm16(in); got != want {
			t.Errorf("pcm16(%v) = %d, want %d", in, got, want)
		}
	}
}
