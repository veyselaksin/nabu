package tts

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

type vector struct {
	Comment  string  `json:"comment"`
	Input    string  `json:"input"`
	AddBlank bool    `json:"add_blank"`
	IDs      []int64 `json:"ids"`
	Error    *struct {
		Symbol string `json:"symbol"`
		Offset int    `json:"offset"`
	} `json:"error"`
}

// TestTokenizeVectors runs the vectors shared with the Python tokenizer
// (tts/train/tests/test_symbols.py), so both sides tokenize identically.
func TestTokenizeVectors(t *testing.T) {
	f, err := os.Open("testdata/tokenize.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	table := DefaultSymbols()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		var v vector
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		if v.Comment != "" {
			continue
		}
		n++
		t.Run(fmt.Sprintf("%q", v.Input), func(t *testing.T) {
			ids, err := table.Tokenize(v.Input)
			if v.Error != nil {
				var unk *UnknownSymbolError
				if !errors.As(err, &unk) || !errors.Is(err, ErrUnknownSymbol) {
					t.Fatalf("err = %v, want UnknownSymbolError", err)
				}
				if got := fmt.Sprintf("U+%04X", unk.Symbol); got != v.Error.Symbol || unk.Offset != v.Error.Offset {
					t.Errorf("error at %s offset %d, want %s offset %d", got, unk.Offset, v.Error.Symbol, v.Error.Offset)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if v.AddBlank {
				ids = table.Intersperse(ids)
			}
			if fmt.Sprint(ids) != fmt.Sprint(v.IDs) {
				t.Errorf("ids = %v, want %v", ids, v.IDs)
			}
		})
	}
	if n == 0 {
		t.Fatal("no vectors")
	}
}

func TestSymbolTableHashMatchesPython(t *testing.T) {
	data, err := os.ReadFile("testdata/symbols-test.txt")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/symbols-test.sha256")
	if err != nil {
		t.Fatal(err)
	}
	table, err := ParseSymbols(strings.NewReader(string(data)), "symbols-test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := table.SHA256(); got != strings.TrimSpace(string(want)) {
		t.Errorf("SHA256 = %s, want %s", got, want)
	}
	if table.Len() != 5 || table.PadID() != 0 {
		t.Errorf("Len = %d, PadID = %d", table.Len(), table.PadID())
	}
	ids, err := table.Tokenize("g ã")
	if err != nil || fmt.Sprint(ids) != "[4 1 2 3]" {
		t.Errorf("Tokenize = %v, %v", ids, err)
	}
}

func TestEmbeddedTableCoversRuleOutputs(t *testing.T) {
	// Every IPA code point the shipped egy-allen rules can emit must be
	// speakable. (The rules are placeholders; this guards the plumbing.)
	table := DefaultSymbols()
	for _, ipa := range []string{"ʕ", "ħ", "χ", "n", "t", "p", "f", "r"} {
		if _, err := table.Tokenize(ipa); err != nil {
			t.Error(err)
		}
	}
}

func TestParseSymbolsErrors(t *testing.T) {
	for name, src := range map[string]string{
		"no pad":          "<space>\na\n",
		"pad not first":   "<space>\n<pad>\n",
		"duplicate":       "<pad>\n<space>\na\nU+0061\n",
		"two runes":       "<pad>\n<space>\nab\n",
		"not NFD":         "<pad>\n<space>\nã\n",
		"lowercase hex":   "<pad>\n<space>\nU+00e3\n",
		"duplicate pad":   "<pad>\n<space>\n<pad>\n",
		"duplicate alias": "<pad>\n<space>\na b\nc b\n",
	} {
		if _, err := ParseSymbols(strings.NewReader(src), name); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
