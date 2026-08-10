package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestStoreSaveOpenRenameAndDelete(t *testing.T) {
	t.Parallel()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	content := "hello over the local network"
	first, err := store.Save(context.Background(), "notes.txt", strings.NewReader(content), 1024)
	if err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256([]byte(content))
	if first.Name != "notes.txt" || first.SHA256 != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("unexpected metadata: %+v", first)
	}

	second, err := store.Save(context.Background(), "notes.txt", strings.NewReader("second"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if second.Name != "notes (1).txt" {
		t.Fatalf("duplicate name = %q", second.Name)
	}

	file, meta, err := store.Open(first.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content || meta != first {
		t.Fatalf("open returned content %q and metadata %+v", got, meta)
	}

	if got := len(store.List()); got != 2 {
		t.Fatalf("list length = %d", got)
	}
	if err := store.Delete(first.Name); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Open(first.Name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("open deleted file error = %v", err)
	}
}

func TestConcurrentUploadsUseUniqueNames(t *testing.T) {
	t.Parallel()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	const workers = 24
	names := make(chan string, workers)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			file, err := store.Save(context.Background(), "concurrent.txt", strings.NewReader("payload"), 1024)
			if err != nil {
				errors <- err
				return
			}
			names <- file.Name
		}()
	}
	group.Wait()
	close(names)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}

	unique := make(map[string]bool, workers)
	for name := range names {
		if unique[name] {
			t.Fatalf("duplicate published filename %q", name)
		}
		unique[name] = true
	}
	if len(unique) != workers || len(store.List()) != workers {
		t.Fatalf("published %d unique files, store lists %d", len(unique), len(store.List()))
	}
}

func BenchmarkStoreSaveAndDelete1MiB(b *testing.B) {
	store, err := New(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	payload := bytes.Repeat([]byte("lan-drop"), (1<<20)/len("lan-drop"))
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		file, err := store.Save(context.Background(), "benchmark.bin", bytes.NewReader(payload), int64(len(payload)))
		if err != nil {
			b.Fatal(err)
		}
		if err := store.Delete(file.Name); err != nil {
			b.Fatal(err)
		}
	}
}

func TestStoreRejectsOversizeAndTraversal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Save(context.Background(), "large.bin", strings.NewReader("12345"), 4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversize error = %v", err)
	}
	if got := len(store.List()); got != 0 {
		t.Fatalf("oversize upload left %d indexed files", got)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), uploadPrefix) {
			t.Fatalf("temporary upload was not removed: %s", entry.Name())
		}
	}

	if _, _, err := store.Open("../secret.txt"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("traversal error = %v", err)
	}
	if err := store.Delete("folder/file.txt"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("nested delete error = %v", err)
	}
	if _, err := store.Save(context.Background(), indexFilename, strings.NewReader("x"), 10); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("reserved filename error = %v", err)
	}
}

func TestStoreReloadsIndexAndDiscoversExistingFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("existing"), 0o640); err != nil {
		t.Fatal(err)
	}

	first, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	files := first.List()
	if len(files) != 1 || files[0].Name != "existing.txt" || files[0].SHA256 == "" {
		t.Fatalf("discovered files = %+v", files)
	}

	second, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	reloaded := second.List()
	if len(reloaded) != 1 || reloaded[0] != files[0] {
		t.Fatalf("reloaded files = %+v, want %+v", reloaded, files)
	}
}
