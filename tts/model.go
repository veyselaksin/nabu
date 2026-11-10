// Package tts synthesizes speech from IPA with Nabu's VITS model, exported to
// ONNX by tts/train (nabu-tts-export) and run with ONNX Runtime through
// github.com/yalue/onnxruntime_go. No Python runs at request time.
//
// The model reads IPA code points as tokens, using the symbol table embedded
// from tts/symbols.txt. A model records the hash of the table it was trained
// with, and Open refuses a model whose hash differs, so the trainer and the
// runtime cannot drift apart.
//
// ONNX Runtime is a shared library loaded at run time (cgo + dlopen). Its
// version must match the C API headers vendored by onnxruntime_go; see
// ORTVersion and `make ort`.
package tts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime"
	"strconv"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// ORTVersion is the ONNX Runtime release that onnxruntime_go v1.38.0 is built
// against. Load exactly this version of the shared library.
const ORTVersion = "1.31.0"

// ONNXFormat is the nabu.format metadata value this package understands.
const ONNXFormat = "1"

var (
	inputNames  = []string{"ipa_token_ids", "speaker_id", "noise_scale", "length_scale", "noise_scale_w"}
	outputNames = []string{"waveform"}
)

// Options configures Open.
type Options struct {
	// LibraryPath is the ONNX Runtime shared library (libonnxruntime.so.1.31.0,
	// libonnxruntime.1.31.0.dylib, onnxruntime.dll). Empty uses
	// DefaultLibraryPath(). Only the first Open in a process sets it.
	LibraryPath string
	// IntraOpThreads limits ONNX Runtime's per-request threads (0 = its default).
	IntraOpThreads int
	// MaxTokens rejects longer inputs (before add_blank) with ErrTooLong.
	// 0 means 2000.
	MaxTokens int
	// Symbols overrides the embedded table (tests only).
	Symbols *SymbolTable
}

// DefaultLibraryPath is the shared library name for this OS, resolved by the
// dynamic loader's normal search path.
func DefaultLibraryPath() string {
	switch runtime.GOOS {
	case "windows":
		return "onnxruntime.dll"
	case "darwin":
		return "libonnxruntime." + ORTVersion + ".dylib"
	default:
		return "libonnxruntime.so." + ORTVersion
	}
}

// Info describes a loaded model.
type Info struct {
	Name          string   `json:"name"`
	Path          string   `json:"-"`
	SampleRate    int      `json:"sample_rate"`
	AddBlank      bool     `json:"add_blank"`
	Speakers      []string `json:"speakers"`
	Languages     []string `json:"languages"`
	SymbolsSHA256 string   `json:"symbols_sha256"`
	NoiseScale    float32  `json:"noise_scale"`
	LengthScale   float32  `json:"length_scale"`
	NoiseScaleW   float32  `json:"noise_scale_w"`
}

// Params are per-request synthesis settings.
type Params struct {
	// Speaker indexes Info.Speakers.
	Speaker int
	// Speed scales speaking rate: 2 is twice as fast. 0 means 1.
	// Must be within [MinSpeed, MaxSpeed].
	Speed float64
}

// Speed limits accepted by Synthesize.
const (
	MinSpeed = 0.5
	MaxSpeed = 2.0
)

// Audio is synthesized mono audio.
type Audio struct {
	Samples    []float32
	SampleRate int
}

// Model is a loaded ONNX TTS model. It is safe for concurrent use.
type Model struct {
	info      Info
	symbols   *SymbolTable
	maxTokens int
	session   *ort.DynamicAdvancedSession
}

var (
	envOnce sync.Once
	envErr  error
)

func initEnvironment(libPath string) error {
	envOnce.Do(func() {
		if libPath == "" {
			libPath = DefaultLibraryPath()
		}
		ort.SetSharedLibraryPath(libPath)
		if err := ort.InitializeEnvironment(); err != nil {
			envErr = fmt.Errorf("tts: loading ONNX Runtime %s from %q: %w (see `make ort`)", ORTVersion, libPath, err)
		}
	})
	return envErr
}

// Open loads an exported model. It validates the model's metadata, its
// input/output signature and its symbol-table hash.
func Open(path string, opts Options) (*Model, error) {
	if err := initEnvironment(opts.LibraryPath); err != nil {
		return nil, err
	}
	symbols := opts.Symbols
	if symbols == nil {
		symbols = DefaultSymbols()
	}
	info, err := readInfo(path)
	if err != nil {
		return nil, err
	}
	if info.SymbolsSHA256 != symbols.SHA256() {
		return nil, fmt.Errorf("tts: %s was trained with a different symbol table (model %.12s…, tts/symbols.txt %.12s…); re-export or retrain it",
			path, info.SymbolsSHA256, symbols.SHA256())
	}
	if err := checkSignature(path); err != nil {
		return nil, err
	}

	so, err := ort.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("tts: %w", err)
	}
	defer so.Destroy()
	if opts.IntraOpThreads > 0 {
		if err := so.SetIntraOpNumThreads(opts.IntraOpThreads); err != nil {
			return nil, fmt.Errorf("tts: %w", err)
		}
	}
	session, err := ort.NewDynamicAdvancedSession(path, inputNames, outputNames, so)
	if err != nil {
		return nil, fmt.Errorf("tts: loading %s: %w", path, err)
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2000
	}
	info.Path = path
	return &Model{info: info, symbols: symbols, maxTokens: maxTokens, session: session}, nil
}

func readInfo(path string) (Info, error) {
	md, err := ort.GetModelMetadata(path)
	if err != nil {
		return Info{}, fmt.Errorf("tts: reading %s: %w", path, err)
	}
	defer md.Destroy()
	get := func(key string) (string, error) {
		v, ok, err := md.LookupCustomMetadataMap(key)
		if err != nil {
			return "", fmt.Errorf("tts: %s: metadata %s: %w", path, key, err)
		}
		if !ok {
			return "", fmt.Errorf("tts: %s: missing metadata %q; was it exported with nabu-tts-export?", path, key)
		}
		return v, nil
	}
	var info Info
	var errs []error
	str := func(key string, dst *string) {
		v, err := get(key)
		errs = append(errs, err)
		*dst = v
	}
	float := func(key string, dst *float32) {
		var s string
		str(key, &s)
		if s != "" {
			f, err := strconv.ParseFloat(s, 32)
			if err != nil {
				errs = append(errs, fmt.Errorf("tts: %s: metadata %s=%q: %w", path, key, s, err))
			}
			*dst = float32(f)
		}
	}
	var format, sr, blank, speakers, languages string
	str("nabu.format", &format)
	str("nabu.model_name", &info.Name)
	str("nabu.symbols_sha256", &info.SymbolsSHA256)
	str("nabu.sample_rate", &sr)
	str("nabu.add_blank", &blank)
	str("nabu.speakers", &speakers)
	str("nabu.languages", &languages)
	float("nabu.noise_scale", &info.NoiseScale)
	float("nabu.length_scale", &info.LengthScale)
	float("nabu.noise_scale_w", &info.NoiseScaleW)
	if err := errors.Join(errs...); err != nil {
		return Info{}, err
	}
	if format != ONNXFormat {
		return Info{}, fmt.Errorf("tts: %s: unsupported nabu.format %q (want %q)", path, format, ONNXFormat)
	}
	if info.SampleRate, err = strconv.Atoi(sr); err != nil || info.SampleRate <= 0 {
		return Info{}, fmt.Errorf("tts: %s: invalid nabu.sample_rate %q", path, sr)
	}
	info.AddBlank = blank == "1"
	if err := json.Unmarshal([]byte(speakers), &info.Speakers); err != nil || len(info.Speakers) == 0 {
		return Info{}, fmt.Errorf("tts: %s: invalid nabu.speakers %q", path, speakers)
	}
	if err := json.Unmarshal([]byte(languages), &info.Languages); err != nil {
		return Info{}, fmt.Errorf("tts: %s: invalid nabu.languages %q", path, languages)
	}
	return info, nil
}

func checkSignature(path string) error {
	ins, outs, err := ort.GetInputOutputInfo(path)
	if err != nil {
		return fmt.Errorf("tts: reading %s: %w", path, err)
	}
	want := map[string]ort.TensorElementDataType{
		"ipa_token_ids": ort.TensorElementDataTypeInt64,
		"speaker_id":    ort.TensorElementDataTypeInt64,
		"noise_scale":   ort.TensorElementDataTypeFloat,
		"length_scale":  ort.TensorElementDataTypeFloat,
		"noise_scale_w": ort.TensorElementDataTypeFloat,
		"waveform":      ort.TensorElementDataTypeFloat,
	}
	seen := map[string]bool{}
	for _, io := range append(ins, outs...) {
		if t, ok := want[io.Name]; ok {
			if io.DataType != t {
				return fmt.Errorf("tts: %s: %s has element type %v, want %v", path, io.Name, io.DataType, t)
			}
			seen[io.Name] = true
		}
	}
	for name := range want {
		if !seen[name] {
			return fmt.Errorf("tts: %s: missing input/output %q; was it exported with nabu-tts-export?", path, name)
		}
	}
	return nil
}

// Info returns the model's metadata.
func (m *Model) Info() Info { return m.info }

// Symbols returns the symbol table the model uses.
func (m *Model) Symbols() *SymbolTable { return m.symbols }

// Synthesize renders IPA to audio. Errors: *UnknownSymbolError (an IPA code
// point not in the table), ErrEmptyInput, ErrTooLong, ErrInvalidParams, or an
// ONNX Runtime error. If ctx is cancelled the run is terminated early.
func (m *Model) Synthesize(ctx context.Context, ipa string, p Params) (*Audio, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("tts: synthesis cancelled: %w", err)
	}
	ids, err := m.symbols.Tokenize(ipa)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, ErrEmptyInput
	}
	if len(ids) > m.maxTokens {
		return nil, fmt.Errorf("%w: %d IPA symbols, the limit is %d", ErrTooLong, len(ids), m.maxTokens)
	}
	if p.Speaker < 0 || p.Speaker >= len(m.info.Speakers) {
		return nil, fmt.Errorf("%w: speaker %d out of range [0, %d]", ErrInvalidParams, p.Speaker, len(m.info.Speakers)-1)
	}
	speed := p.Speed
	if speed == 0 {
		speed = 1
	}
	if math.IsNaN(speed) || speed < MinSpeed || speed > MaxSpeed {
		return nil, fmt.Errorf("%w: speed %g out of range [%g, %g]", ErrInvalidParams, speed, MinSpeed, MaxSpeed)
	}
	if m.info.AddBlank {
		ids = m.symbols.Intersperse(ids)
	}

	var values []ort.Value
	defer func() {
		for _, v := range values {
			if v != nil {
				v.Destroy()
			}
		}
	}()
	add := func(v ort.Value, err error) error {
		if err == nil {
			values = append(values, v)
		}
		return err
	}
	if err := errors.Join(
		add(ort.NewTensor(ort.NewShape(1, int64(len(ids))), ids)),
		add(ort.NewTensor(ort.NewShape(1), []int64{int64(p.Speaker)})),
		add(ort.NewTensor(ort.NewShape(1), []float32{m.info.NoiseScale})),
		add(ort.NewTensor(ort.NewShape(1), []float32{m.info.LengthScale / float32(speed)})),
		add(ort.NewTensor(ort.NewShape(1), []float32{m.info.NoiseScaleW})),
	); err != nil {
		return nil, fmt.Errorf("tts: creating input tensors: %w", err)
	}

	runOpts, err := ort.NewRunOptions()
	if err != nil {
		return nil, fmt.Errorf("tts: %w", err)
	}
	defer runOpts.Destroy()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = runOpts.Terminate()
		case <-done:
		}
	}()

	outputs := []ort.Value{nil}
	if err := m.session.RunWithOptions(values, outputs, runOpts); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("tts: synthesis cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("tts: running model: %w", err)
	}
	values = append(values, outputs[0])
	out, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("tts: unexpected output type %T", outputs[0])
	}
	samples := append([]float32(nil), out.GetData()...)
	return &Audio{Samples: samples, SampleRate: m.info.SampleRate}, nil
}

// Close releases the ONNX Runtime session.
func (m *Model) Close() error {
	if m.session == nil {
		return nil
	}
	err := m.session.Destroy()
	m.session = nil
	return err
}
