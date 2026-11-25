package demo

// Defaults for the built-in fake SimpleFIN bridge.
//
// These are the single-process defaults: the bridge is not part of the public
// router, and binding loopback keeps it reachable from the API process alone.
// Callers dial the base URL below, which is where the API can always find it.
// A Helm install that runs the Temporal sync worker as its own Deployment overrides
// both, setting MOCK_SIMPLEFIN_LISTEN_ADDR to a routable bind address and
// MOCK_SIMPLEFIN_BASE_URL to the in-cluster bridge Service that worker can dial.
// The chart refuses to render a loopback base URL in that topology, because the
// failure would otherwise surface as a broken sync inside the workflow.
const (
	DefaultMockBridgeListenAddr = "127.0.0.1:8090"
	DefaultMockBridgeBaseURL    = "http://127.0.0.1:8090"
)
