package filer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentRename(t *testing.T) {
	store := NewMemoryStore()
	filer := NewFiler(store)
	ctx := context.Background()

	_ = filer.CreateEntry(ctx, &Entry{FullPath: "/a", IsDir: true})
	_ = filer.CreateEntry(ctx, &Entry{FullPath: "/a/b", IsDir: true})
	_ = filer.CreateEntry(ctx, &Entry{FullPath: "/a/b/file1", IsDir: false})
	_ = filer.CreateEntry(ctx, &Entry{FullPath: "/a/b/file2", IsDir: false})

	var wg sync.WaitGroup
	wg.Add(2)

	var err1, err2 error

	go func() {
		defer wg.Done()
		err1 = filer.RenameEntry(ctx, "/a/b", "/a/c")
	}()

	go func() {
		defer wg.Done()
		err2 = filer.RenameEntry(ctx, "/a", "/d")
	}()

	wg.Wait()

	store.mu.RLock()
	defer store.mu.RUnlock()

	t.Logf("Rename 1 (a/b -> a/c) err: %v", err1)
	t.Logf("Rename 2 (a -> d) err: %v", err2)
	t.Logf("Final entries in store:")
	for path := range store.entries {
		t.Logf("  %s", path)
	}

	for path := range store.entries {
		if path == "/" {
			continue
		}
		dir, _ := path.DirAndName()
		if dir != "/" {
			parentPath := FullPath(dir)
			if _, exists := store.entries[parentPath]; !exists {
				t.Errorf("Orphaned entry found: %s (parent %s does not exist)", path, parentPath)
			}
		}
	}
}

func TestSelfReferentialRename(t *testing.T) {
	store := NewMemoryStore()
	filer := NewFiler(store)
	ctx := context.Background()

	_ = filer.CreateEntry(ctx, &Entry{FullPath: "/a", IsDir: true})

	err := filer.RenameEntry(ctx, "/a", "/a/b")
	if err == nil {
		t.Errorf("Expected error when renaming a directory into its own subdirectory")
	}
}

type FailStore struct {
	*MemoryStore
	failInsert bool
}

func (f *FailStore) InsertEntry(ctx context.Context, entry *Entry) error {
	if f.failInsert && strings.Contains(string(entry.FullPath), "file2") {
		return errors.New("simulated insert failure")
	}
	return f.MemoryStore.InsertEntry(ctx, entry)
}

func TestRollbackOnFailure(t *testing.T) {
	memStore := NewMemoryStore()
	failStore := &FailStore{MemoryStore: memStore}
	filer := NewFiler(failStore)
	ctx := context.Background()

	_ = filer.CreateEntry(ctx, &Entry{FullPath: "/a", IsDir: true})
	_ = filer.CreateEntry(ctx, &Entry{FullPath: "/a/file1", IsDir: false})
	_ = filer.CreateEntry(ctx, &Entry{FullPath: "/a/file2", IsDir: false})

	failStore.failInsert = true

	err := filer.RenameEntry(ctx, "/a", "/b")
	if err == nil {
		t.Errorf("Expected rename to fail")
	}

	_, err1 := filer.FindEntry(ctx, "/a")
	_, err2 := filer.FindEntry(ctx, "/a/file1")
	_, err3 := filer.FindEntry(ctx, "/a/file2")
	if err1 != nil || err2 != nil || err3 != nil {
		t.Errorf("Original entries were not fully restored: %v, %v, %v", err1, err2, err3)
	}

	_, err4 := filer.FindEntry(ctx, "/b")
	_, err5 := filer.FindEntry(ctx, "/b/file1")
	if err4 == nil || err5 == nil {
		t.Errorf("New entries were not cleaned up")
	}
}
