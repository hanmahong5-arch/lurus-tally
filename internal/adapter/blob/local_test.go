package blob

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"
)

func TestLocalRoundTripAndRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	const key = "11111111-1111-1111-1111-111111111111/22222222-2222-2222-2222-222222222222"
	body := []byte("png")
	if err := store.Put(context.Background(), key, body, "image/png"); err != nil {
		t.Fatal(err)
	}
	rc, _, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("got %q err %v", got, err)
	}
	outside := filepath.VolumeName(dir) + string(filepath.Separator) + "outside"
	for _, bad := range []string{"../x", "..", outside} {
		if _, err := store.path(bad); err == nil {
			t.Errorf("%s: escaped the root", bad)
		}
	}
}
