package app

import (
	"cmp"
	"context"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/p-society/raag/internal/domain"
)

type inmemoryIndex struct {
	mu            sync.RWMutex
	terms         map[string][]domain.TrackID
	termDF        map[string]int
	termKeys      []string
	trigram       map[string][]domain.TrackID
	trackToks     map[domain.TrackID][]string
	trackTris     map[domain.TrackID][]string
	trackTriCount map[domain.TrackID]int
	charIndex     map[rune][]string
	library       LibraryRepository
	lastUpdated   atomic.Int64
	rebuilding    atomic.Bool
}

type fuzzyMatch struct {
	id       domain.TrackID
	editDist int
}

type scoredResult struct {
	id         domain.TrackID
	tokenScore float64
	editDist   int
}

type tokenHit struct {
	idfScore float64
	count    int
}

type scoredID struct {
	id    domain.TrackID
	score float64
}

const maxEditDistNoMatch = 999

func NewSearchIndex(repo LibraryRepository) SearchIndex {
	return &inmemoryIndex{
		terms:         make(map[string][]domain.TrackID),
		termDF:        make(map[string]int),
		termKeys:      make([]string, 0),
		trigram:       make(map[string][]domain.TrackID),
		trackToks:     make(map[domain.TrackID][]string),
		trackTris:     make(map[domain.TrackID][]string),
		trackTriCount: make(map[domain.TrackID]int),
		charIndex:     make(map[rune][]string),
		library:       repo,
	}
}

func (idx *inmemoryIndex) Index(ctx context.Context, track *domain.Track) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.indexOneLocked(track)
	idx.lastUpdated.Store(time.Now().Unix())
	return nil
}

func (idx *inmemoryIndex) insertSorted(list []domain.TrackID, id domain.TrackID) []domain.TrackID {
	pos, found := slices.BinarySearch(list, id)
	if found {
		return list
	}
	return slices.Insert(list, pos, id)
}

func (idx *inmemoryIndex) removeExistingLocked(id domain.TrackID) {
	if tokens, ok := idx.trackToks[id]; ok {
		for _, token := range tokens {
			newList := removeFromSorted(idx.terms[token], id)
			idx.terms[token] = newList
			if len(newList) == 0 {
				delete(idx.terms, token)
				delete(idx.termDF, token)
				idx.termKeys = removeFromSorted(idx.termKeys, token)
				for _, r := range token {
					list := idx.charIndex[r]
					if pos, found := slices.BinarySearch(list, token); found {
						idx.charIndex[r] = slices.Delete(list, pos, pos+1)
					}
				}
			}
		}
		delete(idx.trackToks, id)
	}
	if tris, ok := idx.trackTris[id]; ok {
		for _, tri := range tris {
			newList := removeFromSorted(idx.trigram[tri], id)
			idx.trigram[tri] = newList
			if len(newList) == 0 {
				delete(idx.trigram, tri)
			}
		}

		delete(idx.trackTris, id)
		delete(idx.trackTriCount, id)
	}
}

func (idx *inmemoryIndex) indexOneLocked(track *domain.Track) {
	idx.removeExistingLocked(track.ID)
	title := track.NormalizedTitle
	if title == "" {
		title = domain.Normalize(track.Title)
	}

	artist := track.NormalizedArtist
	if artist == "" {
		artist = domain.Normalize(track.Artist)
	}

	album := track.NormalizedAlbum
	if album == "" {
		album = domain.Normalize(track.Album)
	}

	tokens := uniqueStrings(tokenize(title + " " + artist + " " + album))
	for _, token := range tokens {
		idx.terms[token] = idx.insertSorted(idx.terms[token], track.ID)
		if pos, found := slices.BinarySearch(idx.termKeys, token); !found {
			idx.termKeys = slices.Insert(idx.termKeys, pos, token)
		}
		idx.termDF[token]++
	}

	idx.trackToks[track.ID] = tokens
	titleTris := trigramsFromNormalizedString(title)
	artistTris := trigramsFromNormalizedString(artist)
	tris := uniqueStrings(slices.Concat(titleTris, artistTris))
	for _, tri := range tris {
		idx.trigram[tri] = idx.insertSorted(idx.trigram[tri], track.ID)
	}

	idx.trackTris[track.ID] = tris
	idx.trackTriCount[track.ID] = len(tris)
	charSeen := make(map[rune]struct{}, len(tokens))
	for _, token := range tokens {
		for _, r := range token {
			if _, ok := charSeen[r]; !ok {
				charSeen[r] = struct{}{}
				list := idx.charIndex[r]
				if pos, found := slices.BinarySearch(list, token); !found {
					idx.charIndex[r] = slices.Insert(list, pos, token)
				}
			}
		}
	}
}

func (idx *inmemoryIndex) IndexBatch(ctx context.Context, tracks []*domain.Track) error {
	const chunkSize = 256
	for i := 0; i < len(tracks); i += chunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}

		end := min(i+chunkSize, len(tracks))
		idx.mu.Lock()
		for _, track := range tracks[i:end] {
			idx.indexOneLocked(track)
		}

		idx.lastUpdated.Store(time.Now().Unix())
		idx.mu.Unlock()
	}
	return nil
}

func (idx *inmemoryIndex) Search(ctx context.Context, query string, limit int) ([]domain.TrackID, error) {
	if idx.rebuilding.Load() {
		return nil, nil
	}

	normalized := domain.Normalize(query)
	if normalized == "" {
		return nil, nil
	}

	queryLen := len(normalized)
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	switch {
	case queryLen == 1:
		return idx.charSearchLocked(normalized, limit), nil
	case queryLen == 2:
		return idx.substring2CharSearchLocked(normalized, limit), nil
	case queryLen <= 4:
		return idx.searchShortQuery(normalized, limit), nil
	default:
		return idx.searchLongQuery(normalized, limit), nil
	}
}

func (idx *inmemoryIndex) searchShortQuery(normalized string, limit int) []domain.TrackID {
	prefixResults := idx.prefixSearchLocked(normalized, limit)
	if len(prefixResults) > 0 {
		return prefixResults
	}

	fuzzyResults := idx.trigramSearchLocked(normalized, limit)
	ids := make([]domain.TrackID, 0, len(fuzzyResults))
	for i, fm := range fuzzyResults {
		if limit > 0 && i >= limit {
			break
		}
		ids = append(ids, fm.id)
	}
	return ids
}

func (idx *inmemoryIndex) searchLongQuery(normalized string, limit int) []domain.TrackID {
	tokens := tokenize(normalized)
	tokenLen := len(tokens)
	candidates := idx.tokenSearchResultsLocked(tokens)

	var exactMatches []scoredID
	var partialMatches map[domain.TrackID]tokenHit
	for id, hit := range candidates {
		if hit.count == tokenLen {
			exactMatches = append(exactMatches, scoredID{id: id, score: hit.idfScore})
		} else if hit.count > 0 {
			if partialMatches == nil {
				partialMatches = make(map[domain.TrackID]tokenHit)
			}
			partialMatches[id] = hit
		}
	}
	if len(exactMatches) > 0 {
		slices.SortFunc(exactMatches, func(a, b scoredID) int {
			if d := cmp.Compare(b.score, a.score); d != 0 {
				return d
			}
			return cmp.Compare(a.id, b.id)
		})

		ids := make([]domain.TrackID, 0, len(exactMatches))
		for i, s := range exactMatches {
			if limit > 0 && i >= limit {
				break
			}
			ids = append(ids, s.id)
		}
		return ids
	}
	if len(partialMatches) > 0 {
		unmatchedTokens := idx.unmatchedTokens(tokens)
		fuzzyResults := idx.assistTrigramSearchLocked(unmatchedTokens)
		fuzzyScores := make(map[domain.TrackID]int)
		for _, fm := range fuzzyResults {
			fuzzyScores[fm.id] = fm.editDist
		}

		results := make([]scoredResult, 0)
		for id, hit := range partialMatches {
			editDist, hasFuzzy := fuzzyScores[id]
			if !hasFuzzy {
				editDist = maxEditDistNoMatch
			}
			results = append(results, scoredResult{id: id, tokenScore: hit.idfScore, editDist: editDist})
		}
		for _, fm := range fuzzyResults {
			if _, exists := partialMatches[fm.id]; !exists {
				results = append(results, scoredResult{id: fm.id, tokenScore: 0, editDist: fm.editDist})
			}
		}

		slices.SortFunc(results, func(a, b scoredResult) int {
			if d := cmp.Compare(b.tokenScore, a.tokenScore); d != 0 {
				return d
			}
			if d := cmp.Compare(a.editDist, b.editDist); d != 0 {
				return d
			}
			return cmp.Compare(a.id, b.id)
		})

		ids := make([]domain.TrackID, 0, len(results))
		for i, r := range results {
			if limit > 0 && i >= limit {
				break
			}
			ids = append(ids, r.id)
		}
		return ids
	}

	fuzzyResults := idx.trigramSearchLocked(normalized, limit)
	ids := make([]domain.TrackID, 0, len(fuzzyResults))
	for i, fm := range fuzzyResults {
		if limit > 0 && i >= limit {
			break
		}
		ids = append(ids, fm.id)
	}
	return ids
}

func (idx *inmemoryIndex) tokenSearchResultsLocked(tokens []string) map[domain.TrackID]tokenHit {
	candidates := make(map[domain.TrackID]tokenHit)
	totalDocs := float64(len(idx.trackToks))
	for _, token := range tokens {
		df := idx.termDF[token]
		if df == 0 {
			continue
		}

		idf := math.Log1p(totalDocs / float64(df))
		if list, ok := idx.terms[token]; ok {
			for _, id := range list {
				hit := candidates[id]
				hit.idfScore += idf
				hit.count++
				candidates[id] = hit
			}
		}
	}
	return candidates
}

func (idx *inmemoryIndex) unmatchedTokens(tokens []string) []string {
	var unmatched []string
	for _, token := range tokens {
		if _, ok := idx.terms[token]; !ok {
			unmatched = append(unmatched, token)
		}
	}
	return unmatched
}

func (idx *inmemoryIndex) assistTrigramSearchLocked(tokens []string) []fuzzyMatch {
	if len(tokens) == 0 {
		return nil
	}
	seen := make(map[string]struct{})

	var allTris []string
	for _, token := range tokens {
		for _, tri := range trigramsFromNormalizedString(token) {
			if _, ok := seen[tri]; !ok {
				seen[tri] = struct{}{}
				allTris = append(allTris, tri)
			}
		}
	}

	trigramCounts := make(map[domain.TrackID]int)
	for _, tri := range allTris {
		if list, ok := idx.trigram[tri]; ok {
			for _, id := range list {
				trigramCounts[id]++
			}
		}
	}

	maxTotalDist := 0
	for _, t := range tokens {
		maxTotalDist += maxEdits(len(t))
	}

	var buf levBuf
	scored := make([]fuzzyMatch, 0)
	for id := range trigramCounts {
		trackToks := idx.trackToks[id]
		if len(trackToks) == 0 {
			continue
		}

		editDist := minEditDistance(tokens, trackToks, &buf)
		if editDist <= maxTotalDist {
			scored = append(scored, fuzzyMatch{id: id, editDist: editDist})
		}
	}

	slices.SortFunc(scored, func(a, b fuzzyMatch) int {
		if d := cmp.Compare(a.editDist, b.editDist); d != 0 {
			return d
		}
		return cmp.Compare(a.id, b.id)
	})
	return scored
}

func (idx *inmemoryIndex) trigramSearchLocked(query string, limit int) []fuzzyMatch {
	rawTris := trigramsFromNormalizedString(query)
	if len(rawTris) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(rawTris))

	var queryTris []string
	for _, tri := range rawTris {
		if _, ok := seen[tri]; !ok {
			seen[tri] = struct{}{}
			queryTris = append(queryTris, tri)
		}
	}

	trigramCounts := make(map[domain.TrackID]int)
	for _, tri := range queryTris {
		if list, ok := idx.trigram[tri]; ok {
			for _, id := range list {
				trigramCounts[id]++
			}
		}
	}

	queryTokens := tokenize(query)
	maxTotalDist := 0
	for _, qt := range queryTokens {
		maxTotalDist += maxEdits(len(qt))
	}

	var buf levBuf
	scored := make([]fuzzyMatch, 0, len(trigramCounts))
	for id := range trigramCounts {
		trackToks := idx.trackToks[id]
		if len(trackToks) == 0 {
			continue
		}

		editDist := minEditDistance(queryTokens, trackToks, &buf)
		if editDist <= maxTotalDist {
			scored = append(scored, fuzzyMatch{id: id, editDist: editDist})
		}
	}

	slices.SortFunc(scored, func(a, b fuzzyMatch) int {
		if d := cmp.Compare(a.editDist, b.editDist); d != 0 {
			return d
		}
		return cmp.Compare(a.id, b.id)
	})

	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	return scored
}

func (idx *inmemoryIndex) charSearchLocked(query string, limit int) []domain.TrackID {
	candidates := make(map[domain.TrackID]float64)
	r, _ := utf8.DecodeRuneInString(query)
	terms, ok := idx.charIndex[r]
	if !ok {
		return nil
	}

	totalDocs := float64(len(idx.trackToks))
	for _, term := range terms {
		df := idx.termDF[term]
		if df == 0 {
			continue
		}

		idf := math.Log1p(totalDocs / float64(df))
		for _, id := range idx.terms[term] {
			candidates[id] += idf
		}
	}

	scored := make([]scoredID, 0, len(candidates))
	for id, score := range candidates {
		scored = append(scored, scoredID{id: id, score: score})
	}

	slices.SortFunc(scored, func(a, b scoredID) int {
		if d := cmp.Compare(b.score, a.score); d != 0 {
			return d
		}
		return cmp.Compare(a.id, b.id)
	})

	ids := make([]domain.TrackID, 0, len(scored))
	for i, s := range scored {
		if limit > 0 && i >= limit {
			break
		}
		ids = append(ids, s.id)
	}
	return ids
}

func (idx *inmemoryIndex) prefixSearchLocked(prefix string, limit int) []domain.TrackID {
	lo, _ := slices.BinarySearch(idx.termKeys, prefix)
	candidates := make(map[domain.TrackID]float64)
	totalDocs := float64(len(idx.trackToks))
	for _, term := range idx.termKeys[lo:] {
		if !strings.HasPrefix(term, prefix) {
			break
		}

		df := idx.termDF[term]
		if df == 0 {
			continue
		}

		idf := math.Log1p(totalDocs / float64(df))
		for _, id := range idx.terms[term] {
			candidates[id] += idf
		}
	}

	scored := make([]scoredID, 0, len(candidates))
	for id, score := range candidates {
		scored = append(scored, scoredID{id: id, score: score})
	}

	slices.SortFunc(scored, func(a, b scoredID) int {
		if d := cmp.Compare(b.score, a.score); d != 0 {
			return d
		}
		return cmp.Compare(a.id, b.id)
	})

	ids := make([]domain.TrackID, 0, len(scored))
	for i, s := range scored {
		if limit > 0 && i >= limit {
			break
		}
		ids = append(ids, s.id)
	}
	return ids
}

func (idx *inmemoryIndex) substring2CharSearchLocked(query string, limit int) []domain.TrackID {
	if len(query) != 2 {
		return nil
	}

	r1, size := utf8.DecodeRuneInString(query)
	r2, _ := utf8.DecodeRuneInString(query[size:])
	terms1 := idx.charIndex[r1]
	terms2 := idx.charIndex[r2]
	candidates := terms1
	if len(terms2) < len(terms1) {
		candidates = terms2
	}
	if len(candidates) == 0 {
		return nil
	}

	hitScores := make(map[domain.TrackID]float64)
	totalDocs := float64(len(idx.trackToks))
	for _, term := range candidates {
		if !strings.Contains(term, query) {
			continue
		}

		df := idx.termDF[term]
		if df == 0 {
			continue
		}

		idf := math.Log1p(totalDocs / float64(df))
		for _, id := range idx.terms[term] {
			hitScores[id] += idf
		}
	}

	scored := make([]scoredID, 0, len(hitScores))
	for id, score := range hitScores {
		scored = append(scored, scoredID{id: id, score: score})
	}

	slices.SortFunc(scored, func(a, b scoredID) int {
		if d := cmp.Compare(b.score, a.score); d != 0 {
			return d
		}
		return cmp.Compare(a.id, b.id)
	})

	ids := make([]domain.TrackID, 0, len(scored))
	for i, s := range scored {
		if limit > 0 && i >= limit {
			break
		}
		ids = append(ids, s.id)
	}
	return ids
}

func (idx *inmemoryIndex) Delete(ctx context.Context, id domain.TrackID) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if tokens, ok := idx.trackToks[id]; ok {
		for _, token := range tokens {
			idx.terms[token] = removeFromSorted(idx.terms[token], id)
			idx.termDF[token]--
			if len(idx.terms[token]) == 0 {
				delete(idx.terms, token)
				if idx.termDF[token] <= 0 {
					delete(idx.termDF, token)
				}

				idx.termKeys = removeFromSorted(idx.termKeys, token)
				for _, r := range token {
					list := idx.charIndex[r]
					if pos, found := slices.BinarySearch(list, token); found {
						idx.charIndex[r] = slices.Delete(list, pos, pos+1)
					}
				}
			}
		}
		delete(idx.trackToks, id)
	}
	if tris, ok := idx.trackTris[id]; ok {
		for _, tri := range tris {
			idx.trigram[tri] = removeFromSorted(idx.trigram[tri], id)
			if len(idx.trigram[tri]) == 0 {
				delete(idx.trigram, tri)
			}
		}

		delete(idx.trackTris, id)
		delete(idx.trackTriCount, id)
	}

	idx.lastUpdated.Store(time.Now().Unix())
	return nil
}

func (idx *inmemoryIndex) Stats(ctx context.Context) (IndexStats, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	stats := IndexStats{
		TotalTracks:   len(idx.trackToks),
		TotalTerms:    len(idx.terms),
		TotalTrigrams: len(idx.trigram),
		LastUpdated:   idx.lastUpdated.Load(),
	}
	return stats, nil
}

func (idx *inmemoryIndex) Rebuild(ctx context.Context, repo LibraryRepository) error {
	idx.rebuilding.Store(true)
	defer idx.rebuilding.Store(false)

	tracks, err := repo.ListAll(ctx)
	if err != nil {
		return err
	}

	tempIdx := &inmemoryIndex{
		terms:         make(map[string][]domain.TrackID),
		termDF:        make(map[string]int),
		termKeys:      make([]string, 0),
		trigram:       make(map[string][]domain.TrackID),
		trackToks:     make(map[domain.TrackID][]string),
		trackTris:     make(map[domain.TrackID][]string),
		trackTriCount: make(map[domain.TrackID]int),
		charIndex:     make(map[rune][]string),
	}
	if err := tempIdx.IndexBatch(ctx, tracks); err != nil {
		return err
	}

	idx.mu.Lock()
	idx.terms = tempIdx.terms
	idx.termDF = tempIdx.termDF
	idx.termKeys = tempIdx.termKeys
	idx.trigram = tempIdx.trigram
	idx.trackToks = tempIdx.trackToks
	idx.trackTris = tempIdx.trackTris
	idx.trackTriCount = tempIdx.trackTriCount
	idx.charIndex = tempIdx.charIndex
	idx.lastUpdated.Store(time.Now().Unix())
	idx.mu.Unlock()

	return nil
}

func tokenize(s string) []string {
	s = domain.Normalize(s)
	if s == "" {
		return nil
	}

	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			return r
		}
		return ' '
	}, s)
	return strings.Fields(s)
}

func trigramsFromNormalizedString(s string) []string {
	if len(s) < 3 {
		return nil
	}

	// Safe to slice by bytes because domain.Normalize outputs ASCII-only.
	// It discards all non-ASCII characters, keeping only a-z, 0-9, and spaces.
	result := make([]string, 0, len(s)-2)
	for i := 0; i <= len(s)-3; i++ {
		result = append(result, s[i:i+3])
	}
	return result
}

func removeFromSorted[S ~[]E, E cmp.Ordered](list S, target E) S {
	pos, found := slices.BinarySearch(list, target)
	if found {
		return slices.Delete(list, pos, pos+1)
	}
	return list
}

func uniqueStrings(in []string) []string {
	if len(in) <= 1 {
		return in
	}

	out := make([]string, len(in))
	copy(out, in)
	slices.Sort(out)
	return slices.Compact(out)
}

func maxEdits(tokenLen int) int {
	switch {
	case tokenLen <= 2:
		return 0
	case tokenLen <= 5:
		return 1
	default:
		return 2
	}
}

type levBuf struct {
	row []int
}

func (b *levBuf) init(n int) {
	if cap(b.row) < n+1 {
		b.row = make([]int, n+1)
	}

	b.row = b.row[:n+1]
	for j := range b.row {
		b.row[j] = j
	}
}

func (b *levBuf) levenshtein(s1 string, s2 string, maxDist int) int {
	if len(s1) < len(s2) {
		s1, s2 = s2, s1
	}

	m, n := len(s1), len(s2)
	diff := m - n
	if diff > maxDist {
		return maxDist + 1
	}

	b.init(n)
	for i := 1; i <= m; i++ {
		rowMin := i
		colStart := max(i-maxDist, 1)
		colEnd := min(i+maxDist, n)
		prev := b.row[colStart-1]
		if colStart > 1 {
			b.row[colStart-1] = maxDist + 1
		}
		for j := colStart; j <= colEnd; j++ {
			oldCur := b.row[j]
			if s1[i-1] == s2[j-1] {
				b.row[j] = prev
			} else {
				b.row[j] = 1 + min(prev, min(oldCur, b.row[j-1]))
			}

			prev = oldCur
			if b.row[j] < rowMin {
				rowMin = b.row[j]
			}
		}
		if rowMin > maxDist {
			return maxDist + 1
		}
	}
	if b.row[n] > maxDist {
		return maxDist + 1
	}
	return b.row[n]
}

func minEditDistance(queryTokens []string, trackTokens []string, buf *levBuf) int {
	totalDist := 0
	for _, qt := range queryTokens {
		k := maxEdits(len(qt))
		bestForToken := k + 1
		for _, tt := range trackTokens {
			dist := buf.levenshtein(qt, tt, k)
			if dist < bestForToken {
				bestForToken = dist
				if bestForToken == 0 {
					break
				}
			}
		}
		totalDist += bestForToken
	}
	return totalDist
}
