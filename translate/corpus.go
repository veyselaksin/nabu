package translate

import (
	"errors"
	"fmt"
	"os"

	"github.com/veyselaksin/nabu/data/source"
	"github.com/veyselaksin/nabu/data/tla"
)

// LoadIndex reads the TLA corpus file downloaded by `nabu-data fetch tla`,
// after checking that it is the pinned version, and indexes it.
func LoadIndex(path string) (*Index, error) {
	src, _ := source.Get("tla")
	if err := source.VerifyFile(path, src.Files[0].SHA256); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s not found; run: make data-fetch", path)
		}
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sentences, err := tla.Read(f)
	if err != nil {
		return nil, err
	}
	return NewIndex(sentences), nil
}
