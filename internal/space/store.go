package space

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/OUBIGFA/FreeMD/internal/fsutil"
)

// Store reads and writes the state as JSON at a path, usually
// <config dir>/FreeMD/spaces.json.
type Store struct{ path string }

// NewStore returns a store of the file at path.
func NewStore(path string) *Store { return &Store{path: path} }

// Path returns the file the store keeps the state in.
func (s *Store) Path() string { return s.path }

// Load reads the state. A missing file is an empty state, not an error.
func (s *Store) Load() (*State, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return &State{Version: CurrentVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("space: reading %s: %w", s.path, err)
	}
	if st.Version > CurrentVersion {
		return nil, fmt.Errorf("space: %s has version %d, newer than this FreeMD understands (%d)", s.path, st.Version, CurrentVersion)
	}
	st.migrate()
	st.Version = CurrentVersion
	return &st, nil
}

// Save writes the state atomically.
func (s *Store) Save(st *State) error {
	st.Version = CurrentVersion
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.path, append(b, '\n'), 0o644)
}
