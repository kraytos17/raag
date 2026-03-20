package domain

import (
	"strings"
	"unicode"
)

func Normalize(s string) string {
	s = strings.ToLower(s)
	s = strings.TrimSpace(s)
	var result strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == ' ' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func NormalizeKey(s string) string {
	var result strings.Builder
	for _, r := range s {
		lower := unicode.ToLower(r)
		if unicode.IsLetter(lower) || r >= '0' && r <= '9' {
			result.WriteRune(lower)
		}
	}
	return result.String()
}
