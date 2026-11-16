package translate

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veyselaksin/nabu/data/corpus"
)

func TestSplitOf(t *testing.T) {
	n := map[string]int{}
	for i := 0; i < 20000; i++ {
		n[SplitOf(fmt.Sprintf("x:%d", i))]++
	}
	if n[SplitTrain] < 17600 || n[SplitTrain] > 18400 || n[SplitValid] < 800 || n[SplitTest] < 800 {
		t.Errorf("split sizes %v", n)
	}
	if SplitOf("x:T1") != SplitOf("x:T1") {
		t.Error("unstable")
	}
}

func TestExportSFT(t *testing.T) {
	tr := New(&fakeLLM{}, fixtureIndex(t), Options{Examples: 2, Glossary: 1, Vectors: testVectors(t), Embedder: &fakeEmbedder{}})
	sents := []corpus.Sentence{
		{ID: "s1", Source: "tla", Stage: "earlier", Translit: "tꞽ ḥnq.t", German: "Brot und Bier", Group: "x:T1", TextID: "T1"},
		{ID: "s2", Source: "aes", Translit: "nswt ꜥnḫ", German: "Der König möge leben", Group: "x:T2", TextID: "T2"},
		{ID: "s3", Source: "tla-late", Stage: "late", Translit: "pꜣ tꞽ", German: "das Brot", Group: "t:pꜣ tꞽ"},
	}
	english := map[string]English{"s1": {ID: "s1", English: "bread and beer", MachineTranslated: true}}
	dir := t.TempDir()
	m, err := ExportSFT(context.Background(), tr, sents, english, dir, SFTOptions{Batch: 2})
	if err != nil && m.Counts[SplitTrain] > 0 {
		t.Fatal(err)
	}
	total := 0
	for _, split := range []string{SplitTrain, SplitValid, SplitTest} {
		ex := readLines[SFTExample](t, filepath.Join(dir, split+".jsonl"))
		meta := readLines[SFTMeta](t, filepath.Join(dir, split+"-meta.jsonl"))
		if len(ex) != len(meta) || len(ex) != m.Counts[split] {
			t.Fatalf("%s: %d examples, %d meta, manifest %d", split, len(ex), len(meta), m.Counts[split])
		}
		for i, e := range ex {
			total++
			mt := meta[i]
			if mt.Split != split || SplitOf(mt.Group) != split || mt.ID == "s3" {
				t.Errorf("meta %+v in %s", mt, split)
			}
			if len(e.Messages) != 3 || e.Messages[2].Role != "assistant" {
				t.Fatalf("messages %+v", e.Messages)
			}
			user := e.Messages[1].Content
			if !strings.HasSuffix(user, mt.Lang+": "+mt.Input) {
				t.Errorf("user prompt ends %q", user[max(0, len(user)-60):])
			}
			// The answer is never among the examples.
			if strings.Contains(user, "→ "+mt.Reference+"\n") {
				t.Errorf("%s leaks its own transliteration:\n%s", mt.ID, user)
			}
			var ans struct{ Transliteration string }
			if err := json.Unmarshal([]byte(e.Messages[2].Content), &ans); err != nil || ans.Transliteration != mt.Reference {
				t.Errorf("assistant %q, reference %q", e.Messages[2].Content, mt.Reference)
			}
			if mt.Lang == "English" && !mt.MachineInput {
				t.Error("English input not marked machine-translated")
			}
		}
	}
	if total != 3 || m.Langs["English"] != 1 || m.Langs["German"] != 2 {
		t.Errorf("total %d, manifest %+v", total, m)
	}
}

func readLines[T any](t *testing.T, path string) []T {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []T
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var v T
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}
