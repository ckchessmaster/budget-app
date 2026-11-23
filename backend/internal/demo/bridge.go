package demo

// Defaults for the built-in fake SimpleFIN bridge.
//
// The bridge listens on a loopback-only address and is not part of the public
// router, so no other pod can reach it. Callers dial the base URL below, which is
// where the API process can always find it. Deployments that run the Temporal sync
// worker in a separate pod must set both MOCK_SIMPLEFIN_LISTEN_ADDR (to a routable
// bind address) and MOCK_SIMPLEFIN_BASE_URL (to where that worker can reach it).
const (
	DefaultMockBridgeListenAddr = "127.0.0.1:8090"
	DefaultMockBridgeBaseURL    = "http://127.0.0.1:8090"
)
