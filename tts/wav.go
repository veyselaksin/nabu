package tts

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// EncodeWAV writes mono float samples as a 16-bit PCM RIFF/WAVE file.
// Samples are clamped to [-1, 1]; NaN is written as silence.
func EncodeWAV(w io.Writer, samples []float32, sampleRate int) error {
	if sampleRate <= 0 {
		return errors.New("tts: sample rate must be positive")
	}
	const (
		channels      = 1
		bitsPerSample = 16
		headerSize    = 44
	)
	dataSize := len(samples) * channels * bitsPerSample / 8
	if uint64(dataSize)+headerSize-8 > math.MaxUint32 {
		return errors.New("tts: audio too long for a WAV file")
	}
	buf := make([]byte, headerSize+dataSize)
	le := binary.LittleEndian
	copy(buf[0:], "RIFF")
	le.PutUint32(buf[4:], uint32(headerSize-8+dataSize))
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	le.PutUint32(buf[16:], 16) // PCM fmt chunk size
	le.PutUint16(buf[20:], 1)  // audio format: PCM
	le.PutUint16(buf[22:], channels)
	le.PutUint32(buf[24:], uint32(sampleRate))
	le.PutUint32(buf[28:], uint32(sampleRate*channels*bitsPerSample/8)) // byte rate
	le.PutUint16(buf[32:], channels*bitsPerSample/8)                    // block align
	le.PutUint16(buf[34:], bitsPerSample)
	copy(buf[36:], "data")
	le.PutUint32(buf[40:], uint32(dataSize))
	for i, s := range samples {
		le.PutUint16(buf[headerSize+2*i:], uint16(pcm16(s)))
	}
	_, err := w.Write(buf)
	return err
}

func pcm16(s float32) int16 {
	f := float64(s)
	switch {
	case math.IsNaN(f):
		return 0
	case f >= 1:
		return math.MaxInt16
	case f <= -1:
		return -math.MaxInt16
	}
	return int16(math.Round(f * math.MaxInt16))
}
