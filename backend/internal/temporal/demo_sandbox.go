package temporal

import "github.com/google/uuid"

// DemoResetWorkflowIDPrefix is the prefix of the durable demo sandbox reset workflow ID.
// The remainder is the workspace the reset was requested for.
const DemoResetWorkflowIDPrefix = "demo-reset-"

// demoSandboxNamespace namespaces the sandbox workspace IDs a reset derives. It is fixed:
// the sandbox is generated from it plus the workflow ID, so it must be identical on every
// replay and on every activity retry.
var demoSandboxNamespace = uuid.MustParse("2f2a1c48-6b0e-4c9f-9c1d-8a7f5e0d3b21")

// DemoSandboxID is the workspace that a demo reset workflow builds.
//
// A reset deletes every demo workspace the caller owns and seeds one replacement under this
// ID, so the sandbox a reset finishes with is not the workspace the reset was asked for,
// and the workspace embedded in the workflow ID no longer exists. Anything that authorizes
// a caller against a reset workflow ID has to derive this rather than compare workspace
// IDs, which is why the rule lives beside the workflow rather than in a handler.
func DemoSandboxID(workflowID string) uuid.UUID {
	return uuid.NewSHA1(demoSandboxNamespace, []byte(workflowID))
}
