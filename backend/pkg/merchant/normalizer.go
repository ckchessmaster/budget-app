package merchant

import (
	"regexp"
	"strings"
	"unicode"
)

type Normalizer interface {
	Clean(raw string) string
}

type standardNormalizer struct{}

func NewStandardNormalizer() Normalizer {
	return &standardNormalizer{}
}

var (
	aggregatorPrefixRe = regexp.MustCompile(`(?i)^(SQ\s*\*|TST\s*\*|PAYPAL\s*\*|SP\s*\*|STRIPE\s*\*|PY\s*\*)\s*`)
	amznOrderSuffixRe  = regexp.MustCompile(`(?i)\s*US\*[A-Z0-9]+`)
	storeLocationRe    = regexp.MustCompile(`(?i)(#\d+|STORE\s+\d+|\b\d{4,}\b)`)
	trailingStateRe    = regexp.MustCompile(`(?i)\s+(ATLANTA\s+GA|SAN\s+FRANCISCO\s+CA|NEW\s+YORK\s+NY|[A-Z]{2}\s+US|US|[A-Z]{2})$`)
	multiSpaceRe       = regexp.MustCompile(`\s+`)
)

func (n *standardNormalizer) Clean(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	// Step 1: Strip payment aggregator prefixes and Amazon order suffixes.
	s = aggregatorPrefixRe.ReplaceAllString(s, "")
	s = amznOrderSuffixRe.ReplaceAllString(s, "")

	// Step 2: Strip store/terminal location numbers.
	s = storeLocationRe.ReplaceAllString(s, " ")

	// Step 3: Strip trailing city/state/country abbreviations.
	s = strings.TrimSpace(s)
	for i := 0; i < 2; i++ {
		next := strings.TrimSpace(trailingStateRe.ReplaceAllString(s, ""))
		if next == s || next == "" {
			break
		}
		s = next
	}

	// Step 4: Normalize whitespace and title-case.
	s = strings.TrimSpace(multiSpaceRe.ReplaceAllString(s, " "))
	return titleCase(s)
}

func titleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		runes := []rune(w)
		capNext := true
		for j, r := range runes {
			if capNext && unicode.IsLetter(r) {
				runes[j] = unicode.ToUpper(r)
				capNext = false
			} else if r == '-' || r == '\'' {
				capNext = true
			} else {
				capNext = false
			}
		}
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}
