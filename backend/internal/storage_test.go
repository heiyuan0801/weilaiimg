package internal

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestLocalStorageRoundTrip(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = storage.Put(context.Background(), "objects/aa/file.txt", strings.NewReader("hello"), 5, "text/plain"); err != nil {
		t.Fatal(err)
	}
	reader, err := storage.Open(context.Background(), "objects/aa/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(data) != "hello" {
		t.Fatalf("unexpected stored data: %q (%v)", data, err)
	}
	if err = storage.Delete(context.Background(), "objects/aa/file.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.Open(context.Background(), "objects/aa/file.txt"); err == nil {
		t.Fatal("expected deleted object to be unavailable")
	}
}

func TestLocalStorageRejectsTraversal(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../escape", "objects/../../escape", "/absolute/path"} {
		if _, err = storage.Put(context.Background(), key, strings.NewReader("blocked"), 7, "text/plain"); err == nil {
			t.Errorf("expected key %q to be rejected", key)
		}
	}
}
