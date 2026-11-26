package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSortOrderPreference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lazysvn", "sort-order")
	if got := loadSortOrder(path); got != sortByPath {
		t.Fatalf("missing preference gave %v, want path sort", got)
	}
	for _, order := range []sortOrder{sortByStatus, sortByPath} {
		if err := saveSortOrder(path, order); err != nil {
			t.Fatal(err)
		}
		if got := loadSortOrder(path); got != order {
			t.Fatalf("loaded preference = %v, want %v", got, order)
		}
	}
	if err := os.WriteFile(path, []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadSortOrder(path); got != sortByPath {
		t.Fatalf("invalid preference gave %v, want path sort", got)
	}
}

func TestSaveSortOrderCleansUpAfterFailedReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sort-order")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveSortOrder(path, sortByStatus); err == nil {
		t.Fatal("expected an error replacing a directory")
	}
	children, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].Name() != "sort-order" || !children[0].IsDir() {
		t.Fatalf("failed replacement left unexpected entries: %v", children)
	}
}
