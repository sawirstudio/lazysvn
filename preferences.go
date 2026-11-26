package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func sortOrderPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lazysvn", "sort-order"), nil
}

func loadSortOrder(path string) sortOrder {
	data, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(data)) == sortByStatus.String() {
		return sortByStatus
	}
	return sortByPath
}

func saveSortOrder(path string, order sortOrder) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".sort-order-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())

	_, writeErr := file.WriteString(order.String() + "\n")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
