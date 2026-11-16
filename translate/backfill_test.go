package translate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veyselaksin/nabu/data/corpus"
)

// batchLLM answers backfill prompts: it echoes "EN:" + German, skipping
// sentences that contain "skip" in batches and failing on "fail".
type batchLLM struct{ calls int }

func (b *batchLLM) Chat(_ context.Context, m []Message, _ ChatOptions) (string, error) {
	b.calls++
	lines := strings.Split(strings.TrimSpace(m[1].Content), "\n")
	var parts []string
	for i, l := range lines {
		_, de, _ := strings.Cut(l, ". ")
		if strings.Contains(de, "fail") {
			return "", errors.New("server down")
		}
		if strings.Contains(de, "skip") && len(lines) > 1 {
			continue
		}
		parts = append(parts, `"`+string(rune('1'+i))+`": "EN:`+de+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func TestBackfillEnglish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "en.jsonl")
	// A previous run left one result and a truncated line.
	if err := os.WriteFile(path, []byte(`{"id":"a","en":"old","model":"m"}`+"\n"+`{"id":"b","en`), 0o644); err != nil {
		t.Fatal(err)
	}
	sents := StageOrder([]corpus.Sentence{
		{ID: "d", Stage: "demotic", German: "Demotisch"},
		{ID: "a", Stage: "earlier", German: "schon da"},
		{ID: "b", Stage: "earlier", German: "Brot"},
		{ID: "c", Stage: "", German: "bitte skip"},
		{ID: "e", Stage: "late", German: ""},
	})
	if sents[0].ID != "a" || sents[2].ID != "c" || sents[4].ID != "d" {
		t.Fatalf("StageOrder = %+v", sents)
	}
	llm := &batchLLM{}
	n, err := BackfillEnglish(context.Background(), llm, sents, path, BackfillOptions{Model: "m", Batch: 3})
	if err != nil || n != 3 {
		t.Fatalf("BackfillEnglish = %d, %v", n, err)
	}
	got, err := ReadEnglish(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["a"].English != "old" || got["b"].English != "EN:Brot" || got["c"].English != "EN:bitte skip" ||
		got["d"].English != "EN:Demotisch" || !got["b"].MachineTranslated || got["b"].Prompt != backfillPromptVersion {
		t.Errorf("en.jsonl = %+v", got)
	}
	// Rerun: nothing left to do.
	calls := llm.calls
	if n, err := BackfillEnglish(context.Background(), llm, sents, path, BackfillOptions{}); n != 0 || err != nil || llm.calls != calls {
		t.Errorf("rerun = %d, %v, %d calls", n, err, llm.calls-calls)
	}
	// A server error stops the run.
	if _, err := BackfillEnglish(context.Background(), llm, []corpus.Sentence{{ID: "x", German: "fail"}}, path, BackfillOptions{}); err == nil {
		t.Error("want error")
	}
}
