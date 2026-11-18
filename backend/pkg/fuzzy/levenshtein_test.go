package fuzzy

import "testing"

func TestLevenshteinAndSimilarity(t *testing.T) {
	if d := LevenshteinDistance("kitten", "sitting"); d != 3 {
		t.Fatalf("LevenshteinDistance(kitten, sitting) = %d, want 3", d)
	}
	if s := Similarity("Burger Bar", "BURGER BAR"); s != 1.0 {
		t.Fatalf("Similarity = %f, want 1.0", s)
	}
	if s := Similarity("Burger Bar", "Target"); s >= 0.5 {
		t.Fatalf("expected low similarity, got %f", s)
	}
}
