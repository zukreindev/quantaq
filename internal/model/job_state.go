package model

type JobState int 

const(
	Ready JobState = iota
	Leased
	Acked
	DLQ
)

func CanTransition(from, to JobState) bool {
	switch from {
	case Ready:
		return to == Leased || to == DLQ
	case Leased:
		return to == Acked || to == Ready || to == DLQ
	default:
		return false
	}
}