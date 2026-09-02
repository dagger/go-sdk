package manifest

import (
	"bytes"
	"fmt"

	"github.com/BurntSushi/toml"
)

type EntrypointKind string

const (
	DangKind   EntrypointKind = "dang"
	ModuleKind EntrypointKind = "module"
)

type Entrypoint struct {
	Kind   EntrypointKind `toml:"kind"`
	Source string         `toml:"source"`
}

// Manifest is a functional builder for dagger-module.toml manifest version 2.
// Each With method returns a new value.
type Manifest struct {
	ManifestVersion int         `toml:"manifestVersion"`
	Name            string      `toml:"name"`
	Entrypoint      *Entrypoint `toml:"entrypoint,omitempty"`
}

func New(name string) Manifest {
	return Manifest{ManifestVersion: 2, Name: name}
}

func (m Manifest) WithEntrypoint(kind EntrypointKind, source string) Manifest {
	m.Entrypoint = &Entrypoint{Kind: kind, Source: source}
	return m
}

func (m Manifest) AsFile() ([]byte, error) {
	if m.ManifestVersion != 2 {
		return nil, fmt.Errorf("manifest version must be 2")
	}
	if m.Name == "" {
		return nil, fmt.Errorf("module name is required")
	}
	if m.Entrypoint == nil || m.Entrypoint.Kind == "" || m.Entrypoint.Source == "" {
		return nil, fmt.Errorf("entrypoint kind and source are required")
	}
	switch m.Entrypoint.Kind {
	case DangKind, ModuleKind:
	default:
		return nil, fmt.Errorf("unsupported entrypoint kind %q", m.Entrypoint.Kind)
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return nil, fmt.Errorf("encode module manifest: %w", err)
	}
	return buf.Bytes(), nil
}
