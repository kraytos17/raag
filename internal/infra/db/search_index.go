package db

import (
	"context"
	"sort"
	"strings"

	"github.com/dgraph-io/badger/v4"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
)

const (
	BoostTitle  = 3.0
	BoostArtist = 2.0
	BoostAlbum  = 1.5
)

type BadgerSearchIndex struct {
	db *DB
}

func NewBadgerSearchIndex(db *DB) *BadgerSearchIndex {
	return &BadgerSearchIndex{db: db}
}

func (idx *BadgerSearchIndex) Index(ctx context.Context, track *domain.Track) error {
	return idx.db.Update(func(txn *badger.Txn) error {
		tokens := idx.tokenize(track.Title + " " + track.Artist + " " + track.Album)
		for _, token := range tokens {
			if err := txn.Set(TermIndexKey(token, track.ID), nil); err != nil {
				return err
			}
		}
		for _, tri := range trigrams(track.Title) {
			if err := txn.Set(TrigramIndexKey(tri, track.ID), nil); err != nil {
				return err
			}
		}
		for _, tri := range trigrams(track.Artist) {
			if err := txn.Set(TrigramIndexKey(tri, track.ID), nil); err != nil {
				return err
			}
		}
		return nil
	})
}

func (idx *BadgerSearchIndex) IndexBatch(ctx context.Context, tracks []*domain.Track) error {
	wb := idx.db.NewWriteBatch()
	defer wb.Cancel()

	for _, track := range tracks {
		tokens := idx.tokenize(track.Title + " " + track.Artist + " " + track.Album)
		for _, token := range tokens {
			if err := wb.Set(TermIndexKey(token, track.ID), nil); err != nil {
				return err
			}
		}
		for _, tri := range trigrams(track.Title) {
			if err := wb.Set(TrigramIndexKey(tri, track.ID), nil); err != nil {
				return err
			}
		}
		for _, tri := range trigrams(track.Artist) {
			if err := wb.Set(TrigramIndexKey(tri, track.ID), nil); err != nil {
				return err
			}
		}
	}
	return wb.Flush()
}

func (idx *BadgerSearchIndex) Search(ctx context.Context, query string, limit int) ([]domain.TrackID, error) {
	tokens := idx.tokenize(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	postingLists := make([]map[domain.TrackID]bool, len(tokens))
	err := idx.db.View(func(txn *badger.Txn) error {
		for i, token := range tokens {
			postingLists[i] = make(map[domain.TrackID]bool)
			prefix := []byte(PrefixIdxTerm + token + ":")
			iter := txn.NewIterator(badger.DefaultIteratorOptions)
			defer iter.Close()

			iter.Seek(prefix)
			for iter.ValidForPrefix(prefix) {
				key := iter.Item().Key()
				id := domain.TrackID(key[len(prefix):])
				postingLists[i][id] = true
				iter.Next()
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	intersection := postingLists[0]
	for i := 1; i < len(postingLists); i++ {
		for id := range intersection {
			if !postingLists[i][id] {
				delete(intersection, id)
			}
		}
	}

	result := make([]domain.TrackID, 0, len(intersection))
	for id := range intersection {
		result = append(result, id)
	}

	sort.Slice(result, func(i, j int) bool {
		return string(result[i]) < string(result[j])
	})

	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (idx *BadgerSearchIndex) SearchFuzzy(ctx context.Context, query string, limit int) ([]domain.TrackID, error) {
	queryTrigrams := trigrams(query)
	if len(queryTrigrams) == 0 {
		return nil, nil
	}

	trigramCounts := make(map[domain.TrackID]int)
	var maxCount int
	err := idx.db.View(func(txn *badger.Txn) error {
		for _, tri := range queryTrigrams {
			prefix := []byte(PrefixIdxTrigram + tri + ":")
			iter := txn.NewIterator(badger.DefaultIteratorOptions)
			defer iter.Close()

			iter.Seek(prefix)
			for iter.ValidForPrefix(prefix) {
				key := iter.Item().Key()
				id := domain.TrackID(key[len(PrefixIdxTrigram)+len(tri)+1:])
				trigramCounts[id]++
				if trigramCounts[id] > maxCount {
					maxCount = trigramCounts[id]
				}
				iter.Next()
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	type scoredID struct {
		id    domain.TrackID
		score float64
	}

	scored := make([]scoredID, 0, len(trigramCounts))
	for id, count := range trigramCounts {
		score := float64(count) / float64(len(queryTrigrams))
		scored = append(scored, scoredID{id, score})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	result := make([]domain.TrackID, 0, len(scored))
	for _, s := range scored {
		result = append(result, s.id)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (idx *BadgerSearchIndex) Delete(ctx context.Context, id domain.TrackID) error {
	return idx.db.Update(func(txn *badger.Txn) error {
		prefix := []byte(PrefixIdxTerm)
		iter := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iter.Close()

		var keysToDelete [][]byte
		iter.Seek(prefix)
		for iter.ValidForPrefix(prefix) {
			key := iter.Item().Key()
			if strings.HasSuffix(string(key), ":"+string(id)) {
				keysToDelete = append(keysToDelete, key)
			}
			iter.Next()
		}
		for _, key := range keysToDelete {
			if err := txn.Delete(key); err != nil {
				return err
			}
		}

		prefix2 := []byte(PrefixIdxTrigram)
		keysToDelete = nil
		iter.Seek(prefix2)
		for iter.ValidForPrefix(prefix2) {
			key := iter.Item().Key()
			if strings.HasSuffix(string(key), ":"+string(id)) {
				keysToDelete = append(keysToDelete, key)
			}
			iter.Next()
		}
		for _, key := range keysToDelete {
			if err := txn.Delete(key); err != nil {
				return err
			}
		}
		return nil
	})
}

func (idx *BadgerSearchIndex) Stats(ctx context.Context) (app.IndexStats, error) {
	var stats app.IndexStats
	err := idx.db.View(func(txn *badger.Txn) error {
		iter := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iter.Close()

		termSet := make(map[string]bool)
		iter.Seek([]byte(PrefixIdxTerm))
		for iter.ValidForPrefix([]byte(PrefixIdxTerm)) {
			key := iter.Item().Key()
			stats.TotalTerms++
			termSet[string(key)] = true
			iter.Next()
		}

		iter.Seek([]byte(PrefixIdxTrigram))
		for iter.ValidForPrefix([]byte(PrefixIdxTrigram)) {
			stats.TotalTrigrams++
			iter.Next()
		}

		iter.Seek([]byte(PrefixTrackData))
		for iter.ValidForPrefix([]byte(PrefixTrackData)) {
			stats.TotalTracks++
			iter.Next()
		}

		_ = termSet
		return nil
	})
	return stats, err
}

func (idx *BadgerSearchIndex) Rebuild(ctx context.Context) error {
	return nil
}

func (idx *BadgerSearchIndex) tokenize(s string) []string {
	s = strings.ToLower(s)
	s = strings.TrimSpace(s)

	var tokens []string
	var current strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			current.WriteByte(c)
		} else if c == ' ' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func trigrams(s string) []string {
	s = strings.ToLower(s)
	var result []string
	for i := 0; i <= len(s)-3; i++ {
		result = append(result, s[i:i+3])
	}
	return result
}

func rank(query string, track *domain.Track, playCount int, lastPlayed int64) float64 {
	matchScore := 0.0
	q := strings.ToLower(query)
	if strings.Contains(strings.ToLower(track.Title), q) {
		matchScore += BoostTitle
	}
	if strings.Contains(strings.ToLower(track.Artist), q) {
		matchScore += BoostArtist
	}
	if strings.Contains(strings.ToLower(track.Album), q) {
		matchScore += BoostAlbum
	}
	return matchScore
}
