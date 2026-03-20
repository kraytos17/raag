package db

import (
	"github.com/p-society/raag/internal/domain"
)

type FileStats struct {
	Added    []string
	Modified []string
	Deleted  []string
}

func diffFileStats(current, previous map[string]*domain.FileStat) FileStats {
	result := FileStats{
		Added:    []string{},
		Modified: []string{},
		Deleted:  []string{},
	}
	for path, currentStat := range current {
		prevStat, exists := previous[path]
		if !exists {
			result.Added = append(result.Added, path)
		} else if currentStat.Changed(prevStat) {
			result.Modified = append(result.Modified, path)
		}
	}
	for path := range previous {
		if _, exists := current[path]; !exists {
			result.Deleted = append(result.Deleted, path)
		}
	}
	return result
}
