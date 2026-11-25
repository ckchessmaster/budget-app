package server

import (
	"testing"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/google/uuid"
)

// The status endpoint is shared by every workflow, so its authorization rule is the only
// thing stopping one workspace from reading another's history by guessing IDs.
func TestMayReadWorkflowStatusAuthorizesTheEmbeddedWorkspace(t *testing.T) {
	mine := uuid.New()
	theirs := uuid.New()

	tests := map[string]struct {
		workflowID string
		want       bool
	}{
		"manual bank sync for my workspace": {
			workflowID: "bank-sync-manual-" + mine.String(),
			want:       true,
		},
		"scheduled bank sync for my workspace": {
			workflowID: "bank-sync-" + mine.String(),
			want:       true,
		},
		"offline replay for my workspace": {
			workflowID: "offline-replay-" + mine.String() + "-1795576725",
			want:       true,
		},
		"bank sync for another workspace": {
			workflowID: "bank-sync-manual-" + theirs.String(),
			want:       false,
		},
		"bare workspace id is not a workflow": {
			workflowID: mine.String(),
			want:       true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := mayReadWorkflowStatus(tc.workflowID, mine); got != tc.want {
				t.Fatalf("mayReadWorkflowStatus(%q) = %v, want %v", tc.workflowID, got, tc.want)
			}
		})
	}
}

// A demo reset deletes the sandbox named in its workflow ID and seeds a replacement under a
// derived ID, so the poll that reports the reset as finished is always made by a caller
// whose workspace is no longer in that ID. Reading it as not-found makes a reset that
// succeeded look like one that failed.
func TestMayReadWorkflowStatusFollowsTheDemoResetDerivation(t *testing.T) {
	oldSandbox := uuid.New()
	otherSandbox := uuid.New()
	resetID := temporalinternal.DemoResetWorkflowIDPrefix + oldSandbox.String()
	derived := temporalinternal.DemoSandboxID(resetID)

	if derived == oldSandbox {
		t.Fatal("the derived sandbox is expected to differ from the workspace the reset was asked for")
	}
	if !mayReadWorkflowStatus(resetID, oldSandbox) {
		t.Fatal("a reset poll made before the swap must still be authorized")
	}
	if !mayReadWorkflowStatus(resetID, derived) {
		t.Fatal("a reset poll made after the swap must be authorized by the sandbox it produced")
	}
	if mayReadWorkflowStatus(resetID, otherSandbox) {
		t.Fatal("an unrelated workspace must not read someone else's reset")
	}

	// The derivation is not a shortcut: matching it means owning the sandbox the reset
	// produced, and finding a workflow ID whose SHA-1 is someone else's workspace is a
	// preimage search. A caller who owns nothing related to the reset gets nothing.
	if mayReadWorkflowStatus("demo-reset-"+uuid.NewString(), uuid.New()) {
		t.Fatal("an unrelated reset id must not be readable by an unrelated workspace")
	}
}

// Two resets in a row each produce a new sandbox, and each poll has to resolve against the
// reset that produced the sandbox its caller is now looking at.
func TestMayReadWorkflowStatusHandlesConsecutiveResets(t *testing.T) {
	first := uuid.New()
	firstReset := temporalinternal.DemoResetWorkflowIDPrefix + first.String()
	second := temporalinternal.DemoSandboxID(firstReset)
	secondReset := temporalinternal.DemoResetWorkflowIDPrefix + second.String()
	third := temporalinternal.DemoSandboxID(secondReset)

	if !mayReadWorkflowStatus(secondReset, third) {
		t.Fatalf("the second reset must be readable by the sandbox it produced")
	}
	if mayReadWorkflowStatus(firstReset, third) {
		t.Fatal("a stale reset poll must not stay authorized once a newer reset replaced its sandbox")
	}
}
