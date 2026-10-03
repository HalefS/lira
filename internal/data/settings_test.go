package data

import (
	"testing"

	"github.com/HalefS/lira/internal/validator"
)

// The SLA threshold is optional, and that is the part worth pinning down: nil has
// to survive validation so a deployment that never sets one can still save the
// other two settings, while zero has to be rejected because it would mark every
// issue that took any time at all as a breach.
func TestValidateSettingsSLAMinutes(t *testing.T) {
	base := func() *Settings {
		return &Settings{DuplicateWindowHours: 24, LCUWindowDays: 7}
	}

	cases := []struct {
		name    string
		sla     *int
		wantErr bool
	}{
		{"unset is allowed", nil, false},
		{"one minute is allowed", intPtr(1), false},
		{"a normal target is allowed", intPtr(30), false},
		{"a long target is allowed", intPtr(480), false},
		{"the maximum is allowed", intPtr(MaxSLAMinutes), false},
		{"zero is rejected", intPtr(0), true},
		{"negative is rejected", intPtr(-5), true},
		{"above the maximum is rejected", intPtr(MaxSLAMinutes + 1), true},
		{"absurd is rejected", intPtr(999999), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := base()
			s.SLAMinutes = tc.sla
			v := validator.New()
			ValidateSettings(v, s)

			if tc.wantErr && v.Valid() {
				t.Errorf("sla_minutes %v: expected a validation error, got none", *tc.sla)
			}
			if !tc.wantErr && !v.Valid() {
				t.Errorf("sla_minutes %v: expected valid, got %v", *tc.sla, v.Errors)
			}
			// When the SLA is the thing being rejected, it must be the field named
			// in the error. A message pointing at duplicate_window_hours would send
			// a manager looking at the wrong box.
			if tc.wantErr && tc.sla != nil {
				if _, ok := v.Errors["sla_minutes"]; !ok {
					t.Errorf("sla_minutes %v: error not attributed to sla_minutes, got %v", *tc.sla, v.Errors)
				}
			}
		})
	}
}

// The SLA is independent of the two thresholds that were already there, so a
// rejection on one must not report the others as invalid.
func TestValidateSettingsSLAIsIndependent(t *testing.T) {
	v := validator.New()
	ValidateSettings(v, &Settings{
		DuplicateWindowHours: 24,
		LCUWindowDays:        7,
		SLAMinutes:           intPtr(0),
	})

	if v.Valid() {
		t.Fatal("expected the zero SLA to be rejected")
	}
	for _, field := range []string{"duplicate_window_hours", "lcu_window_days"} {
		if _, ok := v.Errors[field]; ok {
			t.Errorf("%s reported invalid because of the SLA: %v", field, v.Errors)
		}
	}
}

func intPtr(n int) *int { return &n }
