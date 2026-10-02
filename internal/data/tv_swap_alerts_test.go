package data

import (
	"strings"
	"testing"

	"github.com/HalefS/lira/internal/validator"
)

// A solution is how a manager records what they did about the faulty set, so an
// empty one says nothing and an oversized one is a paste rather than a note.
func TestValidateTVSwapAlertSolution(t *testing.T) {
	tests := []struct {
		name     string
		solution string
		wantOK   bool
	}{
		{"a plain note", "faulty set replaced from storage", true},
		{"whitespace is trimmed before it arrives, but an all-blank note is not a solution", "   ", false},
		{"empty", "", false},
		{"at the limit", strings.Repeat("x", maxTVSwapAlertSolutionLength), true},
		{"one over the limit", strings.Repeat("x", maxTVSwapAlertSolutionLength+1), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := validator.New()
			ValidateTVSwapAlertSolution(v, tc.solution)
			if got := v.Valid(); got != tc.wantOK {
				t.Errorf("Valid() = %v, want %v (errors: %v)", got, tc.wantOK, v.Errors)
			}
		})
	}
}

// The bound is stated as 1000 in the message the UI shows, so a drift between
// the constant and the wording would be a silent lie.
func TestTVSwapAlertSolutionBoundMatchesItsMessage(t *testing.T) {
	v := validator.New()
	ValidateTVSwapAlertSolution(v, strings.Repeat("x", maxTVSwapAlertSolutionLength+1))
	if len(v.Errors) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(v.Errors), v.Errors)
	}
	msg, ok := v.Errors["solution"]
	if !ok {
		t.Fatalf("error is not keyed on 'solution': %v", v.Errors)
	}
	if !strings.Contains(msg, "1000") {
		t.Errorf("message %q does not mention the 1000 character bound", msg)
	}
}
