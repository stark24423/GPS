package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type generatedFile struct {
	path    string
	modTime int64
}

func PruneGeneratedFiles(dir, pattern string, keep int) (int, error) {
	if keep < 0 {
		return 0, fmt.Errorf("keep must be zero or greater")
	}
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return 0, err
	}
	files := make([]generatedFile, 0, len(matches))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return 0, err
		}
		if info.IsDir() {
			continue
		}
		files = append(files, generatedFile{
			path:    path,
			modTime: info.ModTime().UnixNano(),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].modTime == files[j].modTime {
			return files[i].path > files[j].path
		}
		return files[i].modTime > files[j].modTime
	})
	if len(files) <= keep {
		return 0, nil
	}

	deleted := 0
	var joined error
	for _, file := range files[keep:] {
		if err := os.Remove(file.path); err != nil {
			joined = errors.Join(joined, err)
			continue
		}
		deleted++
	}
	return deleted, joined
}
