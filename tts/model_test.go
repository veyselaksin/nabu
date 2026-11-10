package tts

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// TestModelIntegration needs a model exported by nabu-tts-export and the ONNX
// Runtime library. It is skipped unless NABU_TTS_MODEL is set:
//
//	NABU_TTS_MODEL=runs/smoke/model.onnx NABU_ORT_LIB=$PWD/.ort/lib/libonnxruntime.1.31.0.dylib \
//	  go test ./tts -run Integration -v
func TestModelIntegration(t *testing.T) {
	path := os.Getenv("NABU_TTS_MODEL")
	if path == "" {
		t.Skip("NABU_TTS_MODEL not set")
	}
	m, err := Open(path, Options{LibraryPath: os.Getenv("NABU_ORT_LIB")})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	info := m.Info()
	t.Logf("model %s: %d Hz, %d speakers, languages %v", info.Name, info.SampleRate, len(info.Speakers), info.Languages)

	ctx := context.Background()
	a, err := m.Synthesize(ctx, "ʕnχ ħtp", Params{})
	if err != nil {
		t.Fatal(err)
	}
	if a.SampleRate != info.SampleRate || len(a.Samples) < info.SampleRate/20 {
		t.Fatalf("got %d samples at %d Hz", len(a.Samples), a.SampleRate)
	}
	fast, err := m.Synthesize(ctx, "ʕnχ ħtp", Params{Speed: 2, Speaker: len(info.Speakers) - 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(fast.Samples) >= len(a.Samples) {
		t.Errorf("speed 2 gave %d samples, speed 1 gave %d", len(fast.Samples), len(a.Samples))
	}

	if _, err := m.Synthesize(ctx, "ʕnA", Params{}); !errors.Is(err, ErrUnknownSymbol) {
		t.Errorf("unknown symbol: err = %v", err)
	}
	if _, err := m.Synthesize(ctx, "  ", Params{}); !errors.Is(err, ErrEmptyInput) {
		t.Errorf("empty: err = %v", err)
	}
	if _, err := m.Synthesize(ctx, "n", Params{Speaker: len(info.Speakers)}); !errors.Is(err, ErrInvalidParams) {
		t.Errorf("speaker: err = %v", err)
	}
	if _, err := m.Synthesize(ctx, "n", Params{Speed: 5}); !errors.Is(err, ErrInvalidParams) {
		t.Errorf("speed: err = %v", err)
	}
	cancelled, cancel := context.WithTimeout(ctx, time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if _, err := m.Synthesize(cancelled, "ʕnχ ħtp", Params{}); err == nil {
		t.Error("expected an error from a cancelled context")
	}
}
