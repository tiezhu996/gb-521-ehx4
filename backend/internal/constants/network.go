package constants

type NodeType string

const (
	NodeTypeIntake   NodeType = "intake"
	NodeTypeExhaust  NodeType = "exhaust"
	NodeTypeWorkface NodeType = "workface"
	NodeTypeJunction NodeType = "junction"
)

type NodeStatus string

const (
	NodeStatusActive   NodeStatus = "active"
	NodeStatusInactive NodeStatus = "inactive"
	NodeStatusBlocked  NodeStatus = "blocked"
)

func ValidNodeType(value string) bool {
	switch NodeType(value) {
	case NodeTypeIntake, NodeTypeExhaust, NodeTypeWorkface, NodeTypeJunction:
		return true
	default:
		return false
	}
}

func ValidNodeStatus(value string) bool {
	switch NodeStatus(value) {
	case NodeStatusActive, NodeStatusInactive, NodeStatusBlocked:
		return true
	default:
		return false
	}
}

type DoorState string

const (
	DoorStateOpen     DoorState = "open"
	DoorStateClosed   DoorState = "closed"
	DoorStateRegulate DoorState = "regulating"
)

func ValidDoorState(value string) bool {
	switch DoorState(value) {
	case DoorStateOpen, DoorStateClosed, DoorStateRegulate:
		return true
	default:
		return false
	}
}

func DoorResistanceMultiplier(value string) float64 {
	switch DoorState(value) {
	case DoorStateClosed:
		return 1000
	case DoorStateRegulate:
		return 2.5
	default:
		return 1
	}
}

type StoppageStatus string

const (
	StoppageStatusStopping StoppageStatus = "stopping"
	StoppageStatusRestored StoppageStatus = "restored"
)

func ValidStoppageStatus(value string) bool {
	switch StoppageStatus(value) {
	case StoppageStatusStopping, StoppageStatusRestored:
		return true
	default:
		return false
	}
}
