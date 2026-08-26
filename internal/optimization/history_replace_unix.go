//go:build !windows

package optimization

import (
	"fmt"
	"os"
	"path/filepath"
)

func replaceHistoryFile(source, target string) error {
	if err := os.Rename(source, target); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(target))
	if err != nil {
		return fmt.Errorf("open optimization history directory: %w", err)
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return fmt.Errorf("sync optimization history directory: %w", err)
	}
	if err := directory.Close(); err != nil {
		return fmt.Errorf("close optimization history directory: %w", err)
	}
	return nil
}
