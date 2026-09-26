package constants

type StoppageStatus string

const (
	StoppageStatusActive    StoppageStatus = "active"
	StoppageStatusRecovered StoppageStatus = "recovered"
)

func ValidStoppageStatus(value string) bool {
	switch StoppageStatus(value) {
	case StoppageStatusActive, StoppageStatusRecovered:
		return true
	default:
		return false
	}
}
