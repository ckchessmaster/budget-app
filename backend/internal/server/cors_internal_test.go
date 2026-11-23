package server

import (
	"reflect"
	"testing"
)

func TestParseCORSAllowedOrigins(t *testing.T) {
	tests := map[string]struct {
		raw  string
		want []string
	}{
		"empty":                 {raw: "", want: []string{}},
		"single":                {raw: "https://budget.example.com", want: []string{"https://budget.example.com"}},
		"comma separated":       {raw: "https://a.example.com,https://b.example.com", want: []string{"https://a.example.com", "https://b.example.com"}},
		"padded with spaces":    {raw: "  https://a.example.com ,  https://b.example.com  ", want: []string{"https://a.example.com", "https://b.example.com"}},
		"newline separated":     {raw: "https://a.example.com\nhttps://b.example.com", want: []string{"https://a.example.com", "https://b.example.com"}},
		"semicolons":            {raw: "https://a.example.com;https://b.example.com", want: []string{"https://a.example.com", "https://b.example.com"}},
		"trailing separators":   {raw: "https://a.example.com,,", want: []string{"https://a.example.com"}},
		"duplicates collapse":   {raw: "https://a.example.com,https://a.example.com", want: []string{"https://a.example.com", "https://a.example.com"}},
		"localhost dev origins": {raw: "http://localhost:5173,http://127.0.0.1:5173", want: []string{"http://localhost:5173", "http://127.0.0.1:5173"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := parseCORSAllowedOrigins(tc.raw)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseCORSAllowedOrigins(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNormalizeOrigin(t *testing.T) {
	tests := map[string]struct {
		normalized string
		ok         bool
	}{
		"https://budget.example.com":         {normalized: "https://budget.example.com", ok: true},
		"HTTPS://Budget.Example.COM":         {normalized: "https://budget.example.com", ok: true},
		"https://budget.example.com/":        {normalized: "https://budget.example.com", ok: true},
		"http://localhost:5173":              {normalized: "http://localhost:5173", ok: true},
		"https://budget.example.com/api":     {ok: false},
		"https://budget.example.com?x=1":     {ok: false},
		"https://budget.example.com#frag":    {ok: false},
		"*":                                  {ok: false},
		"https://*.example.com":              {ok: false},
		"budget.example.com":                 {ok: false},
		"//budget.example.com":               {ok: false},
		"":                                   {ok: false},
		"   ":                                {ok: false},
		"https://user:pw@budget.example.com": {ok: false},
		"not a url at all":                   {ok: false},
	}

	for raw, tc := range tests {
		t.Run(raw, func(t *testing.T) {
			normalized, ok := normalizeOrigin(raw)
			if ok != tc.ok {
				t.Fatalf("normalizeOrigin(%q) ok = %v, want %v (got %q)", raw, ok, tc.ok, normalized)
			}
			if ok && normalized != tc.normalized {
				t.Fatalf("normalizeOrigin(%q) = %q, want %q", raw, normalized, tc.normalized)
			}
		})
	}
}

func TestNewCORSPolicyDropsInvalidOrigins(t *testing.T) {
	policy := NewCORSPolicy([]string{
		"https://good.example.com",
		"*",
		"https://path.example.com/api",
		"bad url",
		"",
		"  ",
	})

	if !policy.Allowed("https://good.example.com") {
		t.Fatal("expected the valid origin to be allowed")
	}
	for _, rejected := range []string{"https://path.example.com", "*", "https://path.example.com/api", "http://good.example.com"} {
		if policy.Allowed(rejected) {
			t.Fatalf("expected %q to be rejected", rejected)
		}
	}
}
