package filer

import (
	"context"
	"fmt"
	"strings"
)

func (f *Filer) RenameEntry(ctx context.Context, oldPath, newPath FullPath) error {
	if oldPath == newPath {
		return nil
	}

	// Prevent self-referential loops
	if strings.HasPrefix(string(newPath), string(oldPath)+"/") {
		return fmt.Errorf("cannot rename a directory into its own subdirectory")
	}

	// Acquire exclusive locks on both oldPath and newPath
	f.Locks.LockPaths([]string{string(oldPath), string(newPath)}, true)
	defer f.Locks.UnlockPaths([]string{string(oldPath), string(newPath)}, true)

	// Check if destination already exists
	existingDest, err := f.Store.FindEntry(ctx, newPath)
	if err == nil && existingDest != nil {
		return fmt.Errorf("destination path already exists")
	}

	// Find the source entry
	oldEntry, err := f.Store.FindEntry(ctx, oldPath)
	if err != nil {
		return fmt.Errorf("source path not found: %w", err)
	}

	var entriesToMove []*Entry
	entriesToMove = append(entriesToMove, oldEntry)

	if oldEntry.IsDir {
		subEntries, err := f.findAllEntries(ctx, oldPath)
		if err != nil {
			return fmt.Errorf("failed to list source subdirectory entries: %w", err)
		}
		entriesToMove = append(entriesToMove, subEntries...)
	}

	var insertedPaths []FullPath
	var errOccurred error

	// Insert new entries
	for _, entry := range entriesToMove {
		newEntryPath := getNewPath(oldPath, newPath, entry.FullPath)
		newEntry := &Entry{
			FullPath: newEntryPath,
			IsDir:    entry.IsDir,
		}
		err := f.Store.InsertEntry(ctx, newEntry)
		if err != nil {
			errOccurred = err
			break
		}
		insertedPaths = append(insertedPaths, newEntryPath)
	}

	if errOccurred != nil {
		// Rollback: delete all inserted new entries
		for _, path := range insertedPaths {
			_ = f.Store.DeleteEntry(ctx, path)
		}
		return fmt.Errorf("rename failed during insert: %w", errOccurred)
	}

	// Delete old entries in reverse order (children first)
	var deletedPaths []FullPath
	for i := len(entriesToMove) - 1; i >= 0; i-- {
		entry := entriesToMove[i]
		err := f.Store.DeleteEntry(ctx, entry.FullPath)
		if err != nil {
			errOccurred = err
			break
		}
		deletedPaths = append(deletedPaths, entry.FullPath)
	}

	if errOccurred != nil {
		// Rollback deletion: re-insert deleted old entries
		for _, path := range deletedPaths {
			var originalEntry *Entry
			for _, e := range entriesToMove {
				if e.FullPath == path {
					originalEntry = e
					break
				}
			}
			if originalEntry != nil {
				_ = f.Store.InsertEntry(ctx, originalEntry)
			}
		}
		// Delete all inserted new entries
		for _, path := range insertedPaths {
			_ = f.Store.DeleteEntry(ctx, path)
		}
		return fmt.Errorf("rename failed during delete: %w", errOccurred)
	}

	return nil
}

func (f *Filer) findAllEntries(ctx context.Context, dirPath FullPath) ([]*Entry, error) {
	var entries []*Entry
	var recurse func(FullPath) error
	recurse = func(currentPath FullPath) error {
		var dirEntries []*Entry
		err := f.Store.ListDirectoryEntries(ctx, currentPath, "", false, 100000, func(entry *Entry) bool {
			dirEntries = append(dirEntries, entry)
			return true
		})
		if err != nil {
			return err
		}
		for _, entry := range dirEntries {
			entries = append(entries, entry)
			if entry.IsDir {
				if err := recurse(entry.FullPath); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := recurse(dirPath); err != nil {
		return nil, err
	}
	return entries, nil
}

func getNewPath(oldPath, newPath, entryPath FullPath) FullPath {
	if entryPath == oldPath {
		return newPath
	}
	suffix := string(entryPath)[len(string(oldPath)):]
	return FullPath(string(newPath) + suffix)
}
