package data

import (
	"testing"

	"github.com/HalefS/lira/internal/validator"
)

// Roster validation.
//
// The nil case is the important one and it is unreachable over HTTP by design -- the
// handler refuses it with a 422 before Set is called -- which is exactly why it needs a
// test. A rule no request can exercise is a rule nobody maintains, and this one is
// standing between a silently wrong board and a visibly empty one: a nil slice binds as
// SQL NULL, user_id = ANY(NULL) is NULL rather than true, both of Set's statements would
// match nothing, and the write would return 200 having changed nothing at all.
func TestValidateRosterNilIsRefusedNotTreatedAsEmpty(t *testing.T) {
	v := validator.New()
	ValidateRoster(v, nil)

	if v.Valid() {
		t.Fatal("a nil member_ids was accepted; Set would bind SQL NULL and silently " +
			"change nothing while reporting success")
	}
	if _, ok := v.Errors["member_ids"]; !ok {
		t.Errorf("expected the error on member_ids, got %v", v.Errors)
	}
}

// An EMPTY slice is the opposite of nil, and that asymmetry is the reason this function
// exists at all: empty is a decision a manager is entitled to make, and it is the whole
// justification for storing a flag instead of using a membership set.
func TestValidateRosterEmptyIsLegal(t *testing.T) {
	v := validator.New()
	ValidateRoster(v, []int64{})
	if !v.Valid() {
		t.Fatalf("an empty roster was refused: %v", v.Errors)
	}
}

// The two must be distinguishable. A future refactor that "tidies" these into one
// branch would turn an intentional empty board into a 422, or -- worse the other way --
// turn a nil into a silently empty board.
func TestValidateRosterNilAndEmptyDiffer(t *testing.T) {
	nilV, emptyV := validator.New(), validator.New()
	ValidateRoster(nilV, nil)
	ValidateRoster(emptyV, []int64{})
	if nilV.Valid() == emptyV.Valid() {
		t.Error("nil and empty must not be treated alike: one is a malformed request, " +
			"the other is a legal decision to put nobody on the rota")
	}
}

func TestValidateRoster(t *testing.T) {
	cases := []struct {
		name string
		ids  []int64
		ok   bool
	}{
		{"a normal roster", []int64{1, 2, 3}, true},
		{"one member", []int64{1}, true},
		{"zero is not an id", []int64{0}, false},
		{"negative is not an id", []int64{-4}, false},
		{"a duplicate", []int64{7, 7}, false},
		{"a duplicate among many", []int64{1, 2, 2, 3}, false},
		{"good id alongside a bad one", []int64{5, 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := validator.New()
			ValidateRoster(v, tc.ids)
			if got := v.Valid(); got != tc.ok {
				t.Fatalf("valid = %v, want %v (errors: %v)", got, tc.ok, v.Errors)
			}
			if !tc.ok {
				if _, ok := v.Errors["member_ids"]; !ok {
					t.Errorf("expected the error on member_ids, got %v", v.Errors)
				}
			}
		})
	}
}

// The bound is reachable through the API, so it is a real check here and not only a
// database constraint.
func TestValidateRosterTooMany(t *testing.T) {
	v := validator.New()
	tooMany := make([]int64, MaxRotaMembers+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	ValidateRoster(v, tooMany)
	if v.Valid() {
		t.Fatalf("expected %d ids to be refused", MaxRotaMembers+1)
	}

	atLimit := validator.New()
	exactly := make([]int64, MaxRotaMembers)
	for i := range exactly {
		exactly[i] = int64(i + 1)
	}
	ValidateRoster(atLimit, exactly)
	if !atLimit.Valid() {
		t.Fatalf("exactly %d must be allowed: %v", MaxRotaMembers, atLimit.Errors)
	}
}

// MaxRotaMembers must be far above any real team, because a manager of a large property
// must not hit it by accident. If this number ever drops to something plausible as a
// headcount, the bound has stopped being a runaway guard and become a product limit.
func TestMaxRotaMembersIsAGuardNotALimit(t *testing.T) {
	if MaxRotaMembers < 50 {
		t.Errorf("MaxRotaMembers is %d, which is close enough to a real headcount to "+
			"be mistaken for a product limit", MaxRotaMembers)
	}
}
