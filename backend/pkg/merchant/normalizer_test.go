package merchant

import "testing"

func TestStandardNormalizer(t *testing.T) {
	n := NewStandardNormalizer()

	cases := []struct {
		raw  string
		want string
	}{
		{"SQ *COFFEE SHOP #1204", "Coffee Shop"},
		{"AMZN Mktp US*2A4F12", "Amzn Mktp"},
		{"WAL-MART #2820 ATLANTA GA", "Wal-Mart"},
		{"TST* BURGER BISTRO", "Burger Bistro"},
		{"PAYPAL *SPOTIFY 12345 US", "Spotify"},
	}

	for _, tc := range cases {
		got := n.Clean(tc.raw)
		if got != tc.want {
			t.Errorf("Clean(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
