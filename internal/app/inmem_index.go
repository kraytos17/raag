package app

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/p-society/raag/internal/domain"
)

type inmemoryIndex struct {
	mu        sync.RWMutex
	terms     map[string][]domain.TrackID
	trigram   map[string][]domain.TrackID
	trackToks map[domain.TrackID][]string
	trackTris map[domain.TrackID][]string
	library   LibraryRepository
}

func NewSearchIndex(repo LibraryRepository) SearchIndex {
	return &inmemoryIndex{
		terms:     make(map[string][]domain.TrackID),
		trigram:   make(map[string][]domain.TrackID),
		trackToks: make(map[domain.TrackID][]string),
		trackTris: make(map[domain.TrackID][]string),
		library:   repo,
	}
}

func (idx *inmemoryIndex) Index(ctx context.Context, track *domain.Track) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	tokens := tokenize(track.Title + " " + track.Artist + " " + track.Album)
	for _, token := range tokens {
		idx.terms[token] = idx.insertSorted(idx.terms[token], track.ID)
	}

	idx.trackToks[track.ID] = tokens
	titleTris := trigramsFromString(track.Title)
	artistTris := trigramsFromString(track.Artist)
	tris := append(titleTris, artistTris...)
	for _, tri := range tris {
		idx.trigram[tri] = idx.insertSorted(idx.trigram[tri], track.ID)
	}

	idx.trackTris[track.ID] = tris
	return nil
}

func (idx *inmemoryIndex) insertSorted(list []domain.TrackID, id domain.TrackID) []domain.TrackID {
	pos, _ := slices.BinarySearch(list, id)
	return slices.Insert(list, pos, id)
}

func (idx *inmemoryIndex) IndexBatch(ctx context.Context, tracks []*domain.Track) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	for _, track := range tracks {
		tokens := tokenize(track.Title + " " + track.Artist + " " + track.Album)
		for _, token := range tokens {
			idx.terms[token] = idx.insertSorted(idx.terms[token], track.ID)
		}

		idx.trackToks[track.ID] = tokens
		titleTris := trigramsFromString(track.Title)
		artistTris := trigramsFromString(track.Artist)
		tris := append(titleTris, artistTris...)
		for _, tri := range tris {
			idx.trigram[tri] = idx.insertSorted(idx.trigram[tri], track.ID)
		}
		idx.trackTris[track.ID] = tris
	}
	return nil
}

func (idx *inmemoryIndex) Search(ctx context.Context, query string, limit int) ([]domain.TrackID, error) {
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	idx.mu.RLock()
	postingLists := make([][]domain.TrackID, 0, len(tokens))
	for _, token := range tokens {
		if list, ok := idx.terms[token]; ok {
			postingLists = append(postingLists, list)
		} else {
			idx.mu.RUnlock()
			return nil, nil
		}
	}

	idx.mu.RUnlock()
	if len(postingLists) == 0 {
		return nil, nil
	}
	if len(postingLists) == 1 {
		if limit > 0 && len(postingLists[0]) > limit {
			return postingLists[0][:limit], nil
		}
		return postingLists[0], nil
	}

	result := postingLists[0]
	for i := 1; i < len(postingLists); i++ {
		result = mergeIntersect(result, postingLists[i])
		if len(result) == 0 {
			break
		}
	}
	if limit > 0 && len(result) > limit {
		return result[:limit], nil
	}
	return result, nil
}

func mergeIntersect(a, b []domain.TrackID) []domain.TrackID {
	result := make([]domain.TrackID, 0, min(len(a), len(b)))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			result = append(result, a[i])
			i++
			j++
		} else if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	return result
}

func (idx *inmemoryIndex) SearchFuzzy(ctx context.Context, query string, limit int) ([]domain.TrackID, error) {
	queryTris := trigramsFromString(query)
	if len(queryTris) == 0 {
		return nil, nil
	}

	idx.mu.RLock()
	trigramCounts := make(map[domain.TrackID]int)
	for _, tri := range queryTris {
		if list, ok := idx.trigram[tri]; ok {
			for _, id := range list {
				trigramCounts[id]++
			}
		}
	}
	idx.mu.RUnlock()

	type scoredID struct {
		id    domain.TrackID
		score float64
	}

	scored := make([]scoredID, 0, len(trigramCounts))
	for id, count := range trigramCounts {
		score := float64(count) / float64(len(queryTris))
		scored = append(scored, scoredID{id, score})
	}

	slices.SortFunc(scored, func(a, b scoredID) int {
		return cmp.Compare(b.score, a.score)
	})

	result := make([]domain.TrackID, 0, len(scored))
	for i, s := range scored {
		result = append(result, s.id)
		if limit > 0 && i >= limit {
			break
		}
	}
	return result, nil
}

func (idx *inmemoryIndex) Delete(ctx context.Context, id domain.TrackID) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if tokens, ok := idx.trackToks[id]; ok {
		for _, token := range tokens {
			idx.terms[token] = removeTrackID(idx.terms[token], id)
			if len(idx.terms[token]) == 0 {
				delete(idx.terms, token)
			}
		}
		delete(idx.trackToks, id)
	}
	if tris, ok := idx.trackTris[id]; ok {
		for _, tri := range tris {
			idx.trigram[tri] = removeTrackID(idx.trigram[tri], id)
			if len(idx.trigram[tri]) == 0 {
				delete(idx.trigram, tri)
			}
		}
		delete(idx.trackTris, id)
	}
	return nil
}

func (idx *inmemoryIndex) Stats(ctx context.Context) (IndexStats, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	stats := IndexStats{}
	for range idx.terms {
		stats.TotalTerms++
	}
	for range idx.trigram {
		stats.TotalTrigrams++
	}
	return stats, nil
}

func (idx *inmemoryIndex) Rebuild(ctx context.Context, repo LibraryRepository) error {
	tracks, err := repo.ListAll(ctx)
	if err != nil {
		return err
	}
	return idx.IndexBatch(ctx, tracks)
}

func tokenize(s string) []string {
	s = normalizeForIndex(s)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

func normalizeForIndex(q string) string {
	q = strings.ToLower(q)
	var result strings.Builder
	for _, r := range q {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == ' ' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func trigramsFromString(s string) []string {
	s = normalizeForIndex(s)
	runes := []rune(s)
	if len(runes) < 3 {
		return nil
	}

	var result []string
	for i := 0; i <= len(runes)-3; i++ {
		result = append(result, string(runes[i:i+3]))
	}
	return result
}

func removeTrackID(list []domain.TrackID, id domain.TrackID) []domain.TrackID {
	pos, found := slices.BinarySearch(list, id)
	if found {
		return slices.Delete(list, pos, pos+1)
	}
	return list
}
