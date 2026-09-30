package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HalefS/lira/internal/data"
)

// The gate has to let through exactly what the prompt needs and nothing else.
// If a path leaks in, the "you cannot use the app" promise quietly stops being
// true; if one is missing, the prompt cannot be answered and the user is stuck
// with no way out but logging out.
func TestLCUGateAllowsOnlyWhatThePromptNeeds(t *testing.T) {
	mustAllow := []string{
		"/v1/lcu/today",   // what is outstanding
		"/v1/lcu/tests",   // recording the answer
		"/v1/healthcheck", // a monitor must not be locked out by someone's test
	}
	for _, path := range mustAllow {
		if !lcuGateAllows(path) {
			t.Errorf("%s is blocked, but the prompt cannot work without it", path)
		}
	}

	// Everything else the app uses while signed in has to be refused, otherwise
	// the app stays usable with a result outstanding.
	mustBlock := []string{
		"/v1/issues",
		"/v1/users",
		"/v1/stats",
		"/v1/settings",
		"/v1/alerts",
		"/v1/consumables",
		"/v1/consumable-items",
		"/v1/analytics",
		"/v1/issue-types",
		"/v1/reports/daily",
		"/v1/reports/consumables/weekly",
		"/v1/system/status",
		"/v1/lcu/units", // adding a reader is not part of answering today's test
		"/v1/lcu/units/1",
	}
	for _, path := range mustBlock {
		if lcuGateAllows(path) {
			t.Errorf("%s is allowed through the gate, but should be refused", path)
		}
	}
}

// Only the technician who put a reader on trial, or a manager, may answer for
// it. Nobody else -- not even another technician.
func TestCanAnswerFor(t *testing.T) {
	owner := int64(1)
	other := int64(2)
	deletedOwner := (*int64)(nil) // added_by is SET NULL when the user is deleted

	manager := &data.User{ID: 3, Role: "manager"}
	techOwner := &data.User{ID: 1, Role: "technician"}
	techOther := &data.User{ID: 2, Role: "technician"}

	cases := []struct {
		name    string
		user    *data.User
		addedBy *int64
		want    bool
	}{
		{"owner may answer", techOwner, &owner, true},
		{"another technician may not", techOther, &owner, false},
		{"a manager may answer for anyone", manager, &other, true},
		{"a manager may answer for the owner too", manager, &owner, true},
		// A unit whose owner has been deleted has no owner left, so nobody but a
		// manager can answer for it.
		{"orphaned unit, technician cannot", techOther, deletedOwner, false},
		{"orphaned unit, manager can", manager, deletedOwner, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			unit := &data.LCUUnit{ID: 1, AddedBy: tc.addedBy}
			if got := canAnswerFor(tc.user, unit); got != tc.want {
				t.Errorf("canAnswerFor = %v, want %v", got, tc.want)
			}
		})
	}
}

// The gate's refusal has to be distinguishable from an ordinary 403, because the
// frontend opens the daily prompt on one and shows a permission error on the
// other. Both are 403, so the marker in the body is the only thing separating
// them -- and the units have to travel with it so the prompt needs no second call.
func TestLCUGateResponseCarriesThePrompt(t *testing.T) {
	app := &application{}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/issues", nil)

	units := []*data.LCUUnit{{
		ID:       1,
		Serial:   "LCU-88231",
		StartsOn: data.Today(),
		EndsOn:   data.Today(),
		Status:   data.LCUStatusActive,
	}}
	app.lcuDecisionRequiredResponse(w, r, units)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	body := w.Body.String()
	for _, want := range []string{
		lcuTestRequiredCode, // the marker the frontend switches on
		"LCU-88231",         // the reader, so the prompt can be drawn from this
		"today",             // which day is being asked about
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not mention %q:\n%s", want, body)
		}
	}
}
