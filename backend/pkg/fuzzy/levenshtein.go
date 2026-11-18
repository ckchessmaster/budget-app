package fuzzy

import (
	"strings"
	"unicode/utf8"
)

// LevenshteinDistance computes the edit distance between two strings in runes.
func LevenshteinDistance(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			minVal := del
			if ins < minVal {
				minVal = ins
			}
			if sub < minVal {
				minVal = sub
			}
			curr[j] = minVal
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// Similarity returns a normalized similarity score in [0.0, 1.0].
func Similarity(a, b string) float64 {
	na := strings.ToLower(strings.TrimSpace(a))
	nb := strings.ToLower(strings.TrimSpace(b))
	if na == "" && nb == "" {
		return 1.0
	}
	maxLen := utf8.RuneCountInString(na)
	if lenB := utf8.RuneCountInString(nb); lenB > maxLen {
		maxLen = lenB
	}
	if maxLen == 0 {
		return 1.0
	}
	dist := LevenshteinDistance(na, nb)
	return 1.0 - float64(dist)/float64(maxLen)
}
