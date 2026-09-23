package saga

type OrderState string

const (
	StatePending           OrderState = "PENDING"
	StatePaymentAuthorized OrderState = "PAYMENT_AUTHORIZED"
	StateStockReserved     OrderState = "STOCK_RESERVED"
	StatePaymentCaptured   OrderState = "PAYMENT_CAPTURED"
	StateConfirmed         OrderState = "CONFIRMED"
	StateCompensating      OrderState = "COMPENSATING"
	StateCancelled         OrderState = "CANCELLED"
)

type StepName string

const (
	StepAuthorizePayment StepName = "AUTHORIZE_PAYMENT"
	StepReserveStock     StepName = "RESERVE_STOCK"
	StepCapturePayment   StepName = "CAPTURE_PAYMENT"
	StepConfirmOrder     StepName = "CONFIRM_ORDER"
)

type StepResult struct {
	Name      StepName
	Direction string
	Status    string
}

// NextStep returns the next step to execute for the forward or backward path
func NextStep(currentState OrderState, history []StepResult) *StepName {
	if currentState != StateCompensating {
		var step StepName
		switch currentState {
		case StatePending:
			step = StepAuthorizePayment
		case StatePaymentAuthorized:
			step = StepReserveStock
		case StateStockReserved:
			step = StepCapturePayment
		case StatePaymentCaptured:
			step = StepConfirmOrder
		default:
			return nil
		}
		return &step
	}

	// We are in COMPENSATING state. Find the last attempted forward step that can be compensated.
	var toCompensate *StepName
	for i := len(history) - 1; i >= 0; i-- {
		step := history[i]
		if step.Direction == "forward" && (step.Name == StepAuthorizePayment || step.Name == StepReserveStock) {
			// Check if we already successfully compensated this step
			alreadyCompensated := false
			for _, cstep := range history {
				if cstep.Name == step.Name && cstep.Direction == "backward" && cstep.Status == "SUCCEEDED" {
					alreadyCompensated = true
					break
				}
			}
			if !alreadyCompensated {
				name := step.Name
				toCompensate = &name
				break
			}
		}
	}
	return toCompensate
}

// Advance returns the next order state upon successful step completion
func Advance(currentState OrderState, step StepName, success bool, direction string) OrderState {
	if !success {
		return StateCompensating
	}
	if direction == "backward" {
		return currentState // Stay in COMPENSATING until all steps are compensated (handled by NextStep returning nil)
	}
	switch step {
	case StepAuthorizePayment:
		return StatePaymentAuthorized
	case StepReserveStock:
		return StateStockReserved
	case StepCapturePayment:
		return StatePaymentCaptured
	case StepConfirmOrder:
		return StateConfirmed
	}
	return currentState
}

func IsTerminal(state OrderState) bool {
	return state == StateConfirmed || state == StateCancelled
}
