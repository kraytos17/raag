package domain

import (
	"strings"
	"unicode"
)

// Normalize converts a search query to a normalized form.
// It lowercases, trims whitespace, and keeps only ASCII letters, digits, and spaces.
// Used for general text search matching.
func Normalize(s string) string {
	s = strings.ToLower(s)
	s = strings.TrimSpace(s)

	var result strings.Builder
	result.Grow(len(s))
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == ' ' {
			result.WriteRune(r)
		} else if r == '_' || r == '-' || r == '.' {
			result.WriteRune(' ')
		}
	}
	return result.String()
}

// NormalizeKey converts a string to a normalized key for indexing.
// It lowercases using unicode rules, keeps only letters and digits, and removes spaces.
// Used for database key generation where spaces are not allowed.
func NormalizeKey(s string) string {
	var result strings.Builder
	result.Grow(len(s))
	for _, r := range s {
		lower := unicode.ToLower(r)
		if unicode.IsLetter(lower) || r >= '0' && r <= '9' {
			result.WriteRune(lower)
		}
	}
	return result.String()
}
