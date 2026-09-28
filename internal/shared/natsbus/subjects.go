package natsbus

// versioned subjects; bump the suffix instead of changing a payload shape
const (
	SubjExecutionPending = "execution.pending.v1"
	SubjExecutionSettled = "execution.settled.v1"
	SubjExecutionFailed  = "execution.failed.v1"
)
