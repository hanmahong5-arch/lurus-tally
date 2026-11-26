package blob

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Local stores originals on a directory. The key is tenant/id and must not escape the root.
type Local struct{ root string }

// NewLocal creates root if needed.
func NewLocal(root string) (*Local, error) {
	if root == "" {
		root = "data/intake"
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &Local{root: root}, nil
}

func (l *Local) path(key string) (string, error) {
	clean := filepath.Clean(key)
	if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", os.ErrInvalid
	}
	full := filepath.Join(l.root, clean)
	rel, err := filepath.Rel(l.root, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", os.ErrInvalid
	}
	return full, nil
}

// Put writes body under key, replacing any previous bytes.
func (l *Local) Put(_ context.Context, key string, body []byte, _ string) error {
	full, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return err
	}
	return os.WriteFile(full, body, 0o640)
}

// Open reads a stored object. The content type is left to the caller.
func (l *Local) Open(_ context.Context, key string) (io.ReadCloser, string, error) {
	full, err := l.path(key)
	if err != nil {
		return nil, "", err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, "", err
	}
	return f, "", nil
}
