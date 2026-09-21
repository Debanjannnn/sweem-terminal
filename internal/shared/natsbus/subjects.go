package natsbus

// versioned subjects; bump the suffix instead of changing a payload shape
const (
	SubjExecutionPending = "execution.pending.v1"
	SubjExecutionSettled = "execution.settled.v1"
	SubjExecutionFailed  = "execution.failed.v1"

	SubjOracleIndexPublished = "oracle.index_published.v1"

	SubjRiskLiquidationCandidate = "risk.liquidation_candidate.v1"
	SubjRiskBreakerTripped       = "risk.breaker_tripped.v1"

	SubjScheduleSliceDue = "schedule.slice_due.v1"
)
