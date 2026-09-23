package saga

import (
	"testing"
)

func TestNextStep_ForwardPath(t *testing.T) {
	tests := []struct {
		state OrderState
		want  StepName
	}{
		{StatePending, StepAuthorizePayment},
		{StatePaymentAuthorized, StepReserveStock},
		{StateStockReserved, StepCapturePayment},
		{StatePaymentCaptured, StepConfirmOrder},
	}
	for _, tc := range tests {
		got := NextStep(tc.state, nil)
		if got == nil || *got != tc.want {
			t.Errorf("NextStep(%s) = %v, want %s", tc.state, got, tc.want)
		}
	}
}

func TestNextStep_TerminalStatesReturnNil(t *testing.T) {
	for _, state := range []OrderState{StateConfirmed, StateCancelled} {
		if got := NextStep(state, nil); got != nil {
			t.Errorf("NextStep(%s) should be nil for terminal state, got %s", state, *got)
		}
	}
}

func TestNextStep_CompensatesInReverseOrderOfCompletedSteps(t *testing.T) {
	history := []StepResult{
		{Name: StepAuthorizePayment, Direction: "forward", Status: "SUCCEEDED"},
		{Name: StepReserveStock, Direction: "forward", Status: "SUCCEEDED"},
	}

	got := NextStep(StateCompensating, history)
	if got == nil || *got != StepReserveStock {
		t.Fatalf("first compensation should be RESERVE_STOCK, got %v", got)
	}

	history = append(history, StepResult{Name: StepReserveStock, Direction: "backward", Status: "SUCCEEDED"})
	got = NextStep(StateCompensating, history)
	if got == nil || *got != StepAuthorizePayment {
		t.Fatalf("second compensation should be AUTHORIZE_PAYMENT, got %v", got)
	}

	history = append(history, StepResult{Name: StepAuthorizePayment, Direction: "backward", Status: "SUCCEEDED"})
	got = NextStep(StateCompensating, history)
	if got != nil {
		t.Fatalf("all compensations done, should return nil, got %s", *got)
	}
}

func TestNextStep_OnlyCompensatesCompletedSteps(t *testing.T) {
	// Only auth completed. Reserve never ran. Must not try to release stock.
	history := []StepResult{
		{Name: StepAuthorizePayment, Direction: "forward", Status: "SUCCEEDED"},
	}
	got := NextStep(StateCompensating, history)
	if got == nil || *got != StepAuthorizePayment {
		t.Fatalf("should compensate auth (the only completed step), got %v", got)
	}
}

func TestNextStep_CaptureAndConfirmNeverCompensated(t *testing.T) {
	// Capture is past the pivot — it must never appear as a compensation target,
	// even if it somehow ends up in the history.
	history := []StepResult{
		{Name: StepAuthorizePayment, Direction: "forward", Status: "SUCCEEDED"},
		{Name: StepReserveStock, Direction: "forward", Status: "SUCCEEDED"},
		{Name: StepCapturePayment, Direction: "forward", Status: "SUCCEEDED"},
	}
	got := NextStep(StateCompensating, history)
	if got == nil || *got != StepReserveStock {
		t.Fatalf("should skip capture (pivot) and compensate reserve, got %v", got)
	}
}

func TestAdvance_ForwardSuccess(t *testing.T) {
	tests := []struct {
		state OrderState
		step  StepName
		want  OrderState
	}{
		{StatePending, StepAuthorizePayment, StatePaymentAuthorized},
		{StatePaymentAuthorized, StepReserveStock, StateStockReserved},
		{StateStockReserved, StepCapturePayment, StatePaymentCaptured},
		{StatePaymentCaptured, StepConfirmOrder, StateConfirmed},
	}
	for _, tc := range tests {
		got := Advance(tc.state, tc.step, true, "forward")
		if got != tc.want {
			t.Errorf("Advance(%s, %s, true) = %s, want %s", tc.state, tc.step, got, tc.want)
		}
	}
}

func TestAdvance_ForwardFailureGoesToCompensating(t *testing.T) {
	for _, step := range []StepName{StepAuthorizePayment, StepReserveStock, StepCapturePayment} {
		got := Advance(StatePending, step, false, "forward")
		if got != StateCompensating {
			t.Errorf("Advance(_, %s, false, forward) = %s, want COMPENSATING", step, got)
		}
	}
}

func TestAdvance_BackwardSuccessStaysCompensating(t *testing.T) {
	// Individual compensation success keeps state COMPENSATING;
	// the transition to CANCELLED happens when NextStep returns nil.
	got := Advance(StateCompensating, StepReserveStock, true, "backward")
	if got != StateCompensating {
		t.Errorf("backward success should stay COMPENSATING, got %s", got)
	}
}

func TestIsTerminal(t *testing.T) {
	terminal := []OrderState{StateConfirmed, StateCancelled}
	nonTerminal := []OrderState{StatePending, StatePaymentAuthorized, StateStockReserved, StatePaymentCaptured, StateCompensating}

	for _, s := range terminal {
		if !IsTerminal(s) {
			t.Errorf("%s should be terminal", s)
		}
	}
	for _, s := range nonTerminal {
		if IsTerminal(s) {
			t.Errorf("%s should not be terminal", s)
		}
	}
}
