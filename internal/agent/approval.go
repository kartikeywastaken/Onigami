package agent

import "context"

// Approver is a human-in-the-loop gate. Before a sensitive result is passed on,
// the system can call an Approver to let the user review it and decide whether
// to continue. It returns true to proceed, false to hold.
//
// Like Observer, this keeps the engine UI-agnostic: the agent code only knows
// "ask for approval" — whether that's a terminal prompt now or a TUI dialog
// later is the caller's business. A nil Approver means "no gate" (auto-proceed).
//
//   - ctx:     the run context (so a prompt can be cancelled)
//   - role:    which agent produced the content being reviewed
//   - content: the material to show the user (e.g. the PoC + explanation)
type Approver func(ctx context.Context, role Role, content string) bool

// Ask is a nil-safe helper: if a is nil, approval is automatic.
func (a Approver) Ask(ctx context.Context, role Role, content string) bool {
	if a == nil {
		return true
	}
	return a(ctx, role, content)
}
