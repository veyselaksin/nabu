package oraec

import (
	"reflect"
	"strings"
	"testing"
)

func TestReadEtymologies(t *testing.T) {
	got, err := ReadEtymologies(strings.NewReader("\xef\xbb\xbfC5,854495,\nC9,,928\n\nC1494,159410,6439\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Etymology{{"C5", "854495", ""}, {"C9", "", "928"}, {"C1494", "159410", "6439"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if _, err := ReadEtymologies(strings.NewReader("id,egy\n")); err == nil {
		t.Error("want an error for a header row")
	}
}
