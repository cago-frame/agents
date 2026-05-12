package claudecode

import (
	"errors"
	"strings"
	"testing"
)

func TestProcessExitError_ImplementsError(t *testing.T) {
	e := &ProcessExitError{Code: 1, Stderr: "boom"}
	if e.Error() == "" {
		t.Fatalf("empty error string")
	}
	var target *ProcessExitError
	if !errors.As(e, &target) {
		t.Fatalf("errors.As failed")
	}
}

func TestErrBinaryNotFound(t *testing.T) {
	if ErrBinaryNotFound == nil {
		t.Fatalf("nil sentinel")
	}
}

func TestSentinels_Unique(t *testing.T) {
	got := []error{ErrProcessDead, ErrSessionDead, ErrInitTimeout}
	for i := 0; i < len(got); i++ {
		for j := i + 1; j < len(got); j++ {
			if errors.Is(got[i], got[j]) || errors.Is(got[j], got[i]) {
				t.Fatalf("sentinels not distinct: %v vs %v", got[i], got[j])
			}
		}
	}
}

func TestSentinels_ErrorStringMentionsRecovery(t *testing.T) {
	if !strings.Contains(ErrSessionDead.Error(), "session") {
		t.Fatalf("ErrSessionDead message should mention session: %q", ErrSessionDead.Error())
	}
}
