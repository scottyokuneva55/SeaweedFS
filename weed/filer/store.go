package filer

import (
	"context"
	"errors"
	"strings"
	"sync"
)

type FilerStore interface {
	InsertEntry(ctx context.Context, entry *Entry) error
	UpdateEntry(ctx context.Context, entry *Entry) error
	FindEntry(ctx context.Context, path FullPath) (entry *Entry, err error)
	DeleteEntry(ctx context.Context, path FullPath) error
	ListDirectoryEntries(ctx context.Context, dirPath FullPath, startFileName string, includeStartFile bool, limit int, eachEntryFunc func(entry *Entry) bool) error
}

var ErrNotFound = errors.New("entry not found")

type MemoryStore struct {
	mu      sync.RWMutex
	entries map[FullPath]*Entry
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		entries: make(map[FullPath]*Entry),
	}
}

func (m *MemoryStore) InsertEntry(ctx context.Context, entry *Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[entry.FullPath] = entry
	return nil
}

func (m *MemoryStore) UpdateEntry(ctx context.Context, entry *Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.entries[entry.FullPath]; !exists {
		return ErrNotFound
	}
	m.entries[entry.FullPath] = entry
	return nil
}

func (m *MemoryStore) FindEntry(ctx context.Context, path FullPath) (*Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, exists := m.entries[path]
	if !exists {
		return nil, ErrNotFound
	}
	return entry, nil
}

func (m *MemoryStore) DeleteEntry(ctx context.Context, path FullPath) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.entries[path]; !exists {
		return ErrNotFound
	}
	delete(m.entries, path)
	return nil
}

func (m *MemoryStore) ListDirectoryEntries(ctx context.Context, dirPath FullPath, startFileName string, includeStartFile bool, limit int, eachEntryFunc func(entry *Entry) bool) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	dirStr := string(dirPath)
	if dirStr != "/" && !strings.HasSuffix(dirStr, "/") {
		dirStr += "/"
	}

	count := 0
	for path, entry := range m.entries {
		pathStr := string(path)
		if pathStr == dirStr {
			continue
		}
		if strings.HasPrefix(pathStr, dirStr) {
			subPath := pathStr[len(dirStr):]
			if dirStr == "/" && strings.HasPrefix(subPath, "/") {
				subPath = subPath[1:]
			}
			if !strings.Contains(subPath, "/") && subPath != "" {
				if count >= limit {
					break
				}
				if !eachEntryFunc(entry) {
					break
				}
				count++
			}
		}
	}
	return nil
}
