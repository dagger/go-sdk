package naming

import (
	"encoding/json"
	"os"
	"testing"
)

// TestEngineVectors checks the copy against the engine's shared test vectors,
// testdata/vectors.json, copied from engine/naming with the source.
func TestEngineVectors(t *testing.T) {
	data, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Input   string            `json:"input"`
		Formats map[string]string `json:"formats"`
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) == 0 {
		t.Fatal("no vectors")
	}
	formats := map[string]struct {
		casing Casing
		style  AcronymStyle
	}{
		"PASCAL":             {Pascal, Uppercase},
		"PASCAL_CAPITALIZED": {Pascal, Capitalized},
		"CAMEL":              {Camel, Uppercase},
		"CAMEL_CAPITALIZED":  {Camel, Capitalized},
		"SNAKE":              {Snake, Uppercase},
		"SCREAMING_SNAKE":    {ScreamingSnake, Uppercase},
		"KEBAB":              {Kebab, Uppercase},
		"FLAT":               {Flat, Uppercase},
	}
	for _, v := range vectors {
		id, err := Parse(v.Input)
		if err != nil {
			t.Errorf("parse %q: %v", v.Input, err)
			continue
		}
		for name, want := range v.Formats {
			f, ok := formats[name]
			if !ok {
				t.Errorf("unknown format %s in vectors", name)
				continue
			}
			if got := id.Format(f.casing, f.style); got != want {
				t.Errorf("%q in %s: got %q, want %q", v.Input, name, got, want)
			}
		}
	}
}

func TestCheckKnownVersion(t *testing.T) {
	for _, v := range []string{"", "v0.19.0", "v1.0.0-beta.20", "v1.0.0-beta.21", "1.0.0-beta.21", "v1.0.0-dev-123", "v1.0.0", "v1.0.0+abc", "devel"} {
		if err := CheckKnownVersion(v); err != nil {
			t.Errorf("%q: %v", v, err)
		}
	}
	for _, v := range []string{"v1.0.1", "v1.1.0-beta.1", "v2.0.0"} {
		if err := CheckKnownVersion(v); err == nil {
			t.Errorf("%q: want an error", v)
		}
	}
}
