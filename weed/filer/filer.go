package filer

import (
	"context"
	"strings"
	"sync"
)

type FullPath string

func (p FullPath) DirAndName() (string, string) {
	if p == "/" {
		return "/", ""
	}
	s := string(p)
	if strings.HasSuffix(s, "/") {
		s = s[:len(s)-1]
	}
	i := strings.LastIndex(s, "/")
	if i < 0 {
		return "/", s
	}
	if i == 0 {
		return "/", s[1:]
	}
	return s[:i], s[i+1:]
}

type Entry struct {
	FullPath FullPath
	IsDir    bool
}

type PathLockManager struct {
	mu    sync.Mutex
	locks map[string]*pathLockState
	cond  *sync.Cond
}

type pathLockState struct {
	sharedCount int
	exclusive   bool
}

func NewPathLockManager() *PathLockManager {
	lm := &PathLockManager{
		locks: make(map[string]*pathLockState),
	}
	lm.cond = sync.NewCond(&lm.mu)
	return lm
}

func isDescendantOrEqual(a, b string) bool {
	if a == b {
		return true
	}
	return strings.HasPrefix(a, b+"/")
}

func (lm *PathLockManager) conflicts(path string, exclusive bool) bool {
	for lockedPath, state := range lm.locks {
		overlap := isDescendantOrEqual(path, lockedPath) || isDescendantOrEqual(lockedPath, path)
		if overlap {
			if exclusive || state.exclusive {
				return true
			}
		}
	}
	return false
}

func (lm *PathLockManager) LockPaths(paths []string, exclusive bool) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	for {
		conflict := false
		for _, path := range paths {
			if lm.conflicts(path, exclusive) {
				conflict = true
				break
			}
		}
		if !conflict {
			break
		}
		lm.cond.Wait()
	}

	for _, path := range paths {
		state, exists := lm.locks[path]
		if !exists {
			state = &pathLockState{}
			lm.locks[path] = state
		}
		if exclusive {
			state.exclusive = true
		} else {
			state.sharedCount++
		}
	}
}

func (lm *PathLockManager) UnlockPaths(paths []string, exclusive bool) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	for _, path := range paths {
		state, exists := lm.locks[path]
		if !exists {
			continue
		}
		if exclusive {
			state.exclusive = false
		} else {
			state.sharedCount--
		}
		if !state.exclusive && state.sharedCount == 0 {
			delete(lm.locks, path)
		}
	}
	lm.cond.Broadcast()
}

type Filer struct {
	Store FilerStore
	Locks *PathLockManager
}

func NewFiler(store FilerStore) *Filer {
	return &Filer{
		Store: store,
		Locks: NewPathLockManager(),
	}
}

func (f *Filer) CreateEntry(ctx context.Context, entry *Entry) error {
	pathStr := string(entry.FullPath)
	f.Locks.LockPaths([]string{pathStr}, true)
	defer f.Locks.UnlockPaths([]string{pathStr}, true)

	return f.Store.InsertEntry(ctx, entry)
}

func (f *Filer) UpdateEntry(ctx context.Context, entry *Entry) error {
	pathStr := string(entry.FullPath)
	f.Locks.LockPaths([]string{pathStr}, true)
	defer f.Locks.UnlockPaths([]string{pathStr}, true)

	return f.Store.UpdateEntry(ctx, entry)
}

func (f *Filer) FindEntry(ctx context.Context, path FullPath) (*Entry, error) {
	pathStr := string(path)
	f.Locks.LockPaths([]string{pathStr}, false)
	defer f.Locks.UnlockPaths([]string{pathStr}, false)

	return f.Store.FindEntry(ctx, path)
}

func (f *Filer) DeleteEntry(ctx context.Context, path FullPath) error {
	pathStr := string(path)
	f.Locks.LockPaths([]string{pathStr}, true)
	defer f.Locks.UnlockPaths([]string{pathStr}, true)

	return f.Store.DeleteEntry(ctx, path)
}

func (f *Filer) ListDirectoryEntries(ctx context.Context, dirPath FullPath, startFileName string, includeStartFile bool, limit int, eachEntryFunc func(entry *Entry) bool) error {
	pathStr := string(dirPath)
	f.Locks.LockPaths([]string{pathStr}, false)
	defer f.Locks.UnlockPaths([]string{pathStr}, false)

	return f.Store.ListDirectoryEntries(ctx, dirPath, startFileName, includeStartFile, limit, eachEntryFunc)
}
