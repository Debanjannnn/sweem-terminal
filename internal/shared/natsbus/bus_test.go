package natsbus

import (
	"testing"
	"time"
)

func TestBackoff(t *testing.T) {
	tests := []struct {
		attempt uint64
		want    time.Duration
	}{
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
		{9, 16 * time.Second}, // clamped at maxDeliver, must not overflow the shift
	}
	for _, tt := range tests {
		if got := backoff(tt.attempt); got != tt.want {
			t.Fatalf("backoff(%d) = %v want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestSubjectsCoveredByStream(t *testing.T) {
	subjects := []string{
		SubjExecutionPending, SubjExecutionSettled, SubjExecutionFailed,
	}
	for _, s := range subjects {
		if !covered(s) {
			t.Fatalf("subject %q is not captured by the stream filters %v", s, streamSubjects)
		}
	}
}

func covered(subject string) bool {
	for _, filter := range streamSubjects {
		prefix := filter[:len(filter)-1] // strip the trailing >
		if len(subject) > len(prefix) && subject[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
