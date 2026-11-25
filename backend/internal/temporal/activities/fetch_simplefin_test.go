package activities

import (
	"strings"
	"testing"
)

// The demo seeder stores the bridge address it happened to be configured with, so the
// worker cannot trust the authority in a stored demo URL. These cases are the addresses
// that row can realistically hold after a deploy.
func TestRebaseMockBridgeURLKeepsDemoConnectionsDialable(t *testing.T) {
	const workspaceID = "bb2ffcc5-3555-40af-a1c3-da37b0f4264c"
	const service = "http://budget-app-budget-app-backend-bridge:8090"

	tests := map[string]struct {
		stored string
		base   string
		want   string
	}{
		"sandbox seeded before the worker left the API pod": {
			stored: "http://demouser:demopass@127.0.0.1:8090/mock-simplefin/" + workspaceID + "/accounts",
			base:   service,
			want:   "http://demouser:demopass@budget-app-budget-app-backend-bridge:8090/mock-simplefin/" + workspaceID + "/accounts",
		},
		"release renamed under the sandbox": {
			stored: "http://demouser:demopass@budget-app-old-backend-bridge:8090/mock-simplefin/" + workspaceID + "/accounts",
			base:   service,
			want:   "http://demouser:demopass@budget-app-budget-app-backend-bridge:8090/mock-simplefin/" + workspaceID + "/accounts",
		},
		"bridge moved to another port": {
			stored: "http://demouser:demopass@127.0.0.1:8090/mock-simplefin/" + workspaceID + "/accounts",
			base:   "http://bridge.internal:18090",
			want:   "http://demouser:demopass@bridge.internal:18090/mock-simplefin/" + workspaceID + "/accounts",
		},
		"already addressing the configured bridge": {
			stored: "http://demouser:demopass@budget-app-budget-app-backend-bridge:8090/mock-simplefin/" + workspaceID + "/accounts",
			base:   service,
			want:   "http://demouser:demopass@budget-app-budget-app-backend-bridge:8090/mock-simplefin/" + workspaceID + "/accounts",
		},
		"local run where worker and API share a host": {
			stored: "http://demouser:demopass@127.0.0.1:8090/mock-simplefin/" + workspaceID + "/accounts",
			base:   "http://127.0.0.1:8090",
			want:   "http://demouser:demopass@127.0.0.1:8090/mock-simplefin/" + workspaceID + "/accounts",
		},
		"bridge mounted under a path prefix": {
			stored: "http://demouser:demopass@127.0.0.1:8090/mock-simplefin/" + workspaceID + "/accounts",
			base:   "http://gateway.internal/bridge/",
			want:   "http://demouser:demopass@gateway.internal/bridge/mock-simplefin/" + workspaceID + "/accounts",
		},
		"a real bank keeps its own authority": {
			stored: "https://user:pass@production.simplefin.org/access/0123456789abcdef",
			base:   service,
			want:   "https://user:pass@production.simplefin.org/access/0123456789abcdef",
		},
		"unconfigured base leaves the row alone": {
			stored: "http://demouser:demopass@127.0.0.1:8090/mock-simplefin/" + workspaceID + "/accounts",
			base:   "  ",
			want:   "http://demouser:demopass@127.0.0.1:8090/mock-simplefin/" + workspaceID + "/accounts",
		},
		"base without a host is not an address": {
			stored: "http://demouser:demopass@127.0.0.1:8090/mock-simplefin/" + workspaceID + "/accounts",
			base:   "not a url",
			want:   "http://demouser:demopass@127.0.0.1:8090/mock-simplefin/" + workspaceID + "/accounts",
		},
		"unparseable stored url is returned for the client to reject": {
			stored: "http://%zz/mock-simplefin/" + workspaceID + "/accounts",
			base:   service,
			want:   "http://%zz/mock-simplefin/" + workspaceID + "/accounts",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := rebaseMockBridgeURL(tc.stored, tc.base); got != tc.want {
				t.Fatalf("rebaseMockBridgeURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The credential pair is what the bridge authenticates with, so a rewrite that dropped it
// would trade a connection error for an auth error that looks like a broken sandbox.
func TestRebaseMockBridgeURLPreservesCredentialsAndPath(t *testing.T) {
	const workspaceID = "bb2ffcc5-3555-40af-a1c3-da37b0f4264c"

	got := rebaseMockBridgeURL(
		"http://demouser:demopass@127.0.0.1:8090/mock-simplefin/"+workspaceID+"/accounts",
		"http://budget-app-budget-app-backend-bridge:8090",
	)
	if want := "demouser:demopass@"; !strings.Contains(got, want) {
		t.Fatalf("rewritten url %q lost its credential pair, want it to contain %q", got, want)
	}
	if want := "/mock-simplefin/" + workspaceID + "/accounts"; !strings.Contains(got, want) {
		t.Fatalf("rewritten url %q lost its path, want it to contain %q", got, want)
	}
}
