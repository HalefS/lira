package data

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

func TestClampNotificationLimit(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero falls back to the page size", 0, DefaultNotificationLimit},
		{"negative falls back to the page size", -5, DefaultNotificationLimit},
		{"one is allowed", 1, 1},
		{"a page is left alone", 20, 20},
		{"the ceiling is allowed", MaxNotificationLimit, MaxNotificationLimit},
		{"beyond the ceiling is clamped", 101, MaxNotificationLimit},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClampNotificationLimit(tc.in); got != tc.want {
				t.Errorf("ClampNotificationLimit(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidateNotificationRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     NotificationRef
		wantErr string
	}{
		{"valid maintenance due", NotificationRef{Kind: KindMaintenanceDue, SourceID: 4}, ""},
		{"valid low stock carries its threshold", NotificationRef{Kind: KindLowStock, SourceID: 4, Variant: "6"}, ""},
		{"unknown kind", NotificationRef{Kind: "made_up", SourceID: 4}, "kind"},
		{"zero id", NotificationRef{Kind: KindSLABreach, SourceID: 0}, "source_id"},
		{"negative id", NotificationRef{Kind: KindSLABreach, SourceID: -1}, "source_id"},
		{"low stock without a threshold", NotificationRef{Kind: KindLowStock, SourceID: 4}, "variant"},
		{"low stock with a non-numeric threshold", NotificationRef{Kind: KindLowStock, SourceID: 4, Variant: "six"}, "variant"},
		{"low stock with a leading zero", NotificationRef{Kind: KindLowStock, SourceID: 4, Variant: "06"}, "variant"},
		{"a threshold on a kind that has none", NotificationRef{Kind: KindMaintenanceDue, SourceID: 4, Variant: "6"}, "variant"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := validator.New()
			ValidateNotificationRef(v, tc.ref)
			if tc.wantErr == "" {
				if !v.Valid() {
					t.Fatalf("expected valid, got %v", v.Errors)
				}
				return
			}
			if _, ok := v.Errors[tc.wantErr]; !ok {
				t.Fatalf("expected an error on %q, got %v", tc.wantErr, v.Errors)
			}
		})
	}
}

func TestNotificationKindValid(t *testing.T) {
	valid := []NotificationKind{
		KindMaintenanceDue, KindMaintenanceFault, KindRecurringAlert,
		KindTVSwapAlert, KindLowStock, KindSLABreach,
	}
	for _, k := range valid {
		if !k.Valid() {
			t.Errorf("%q should be valid", k)
		}
	}
	for _, k := range []NotificationKind{"", "MAINTENANCE_DUE", "digest"} {
		if k.Valid() {
			t.Errorf("%q should not be valid", k)
		}
	}
}

// The item JSON is the contract with a client that translates it. Pinning the
// shape stops a server-composed title or message from creeping in, and stops the
// low-stock threshold from being dropped from identity.
func TestNotificationJSONShape(t *testing.T) {
	n := Notification{
		Kind:       KindLowStock,
		SourceID:   7,
		Variant:    "10",
		Severity:   SeverityWarning,
		OccurredAt: time.Date(2026, time.November, 3, 9, 30, 0, 0, time.UTC),
		Unread:     true,
		Data:       map[string]any{"item_name": "Batteries", "stock": 3, "reorder_level": 10},
	}
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{`"kind":"low_stock"`, `"variant":"10"`, `"severity":"warning"`, `"unread":true`, `"item_name":"Batteries"`} {
		if !strings.Contains(got, want) {
			t.Errorf("JSON missing %s: %s", want, got)
		}
	}
	for _, forbidden := range []string{`"title"`, `"body"`, `"message"`} {
		if strings.Contains(got, forbidden) {
			t.Errorf("JSON must not carry display copy, found %s: %s", forbidden, got)
		}
	}

	// Variant is omitted when empty, so an identity that has no threshold cannot
	// be mistaken for one with an empty string as its threshold.
	plain, err := json.Marshal(Notification{Kind: KindSLABreach, SourceID: 1, Data: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), `"variant"`) {
		t.Errorf("empty variant should be omitted: %s", plain)
	}
}

func TestDigestWindowFor(t *testing.T) {
	loc := time.FixedZone("WEST", -3600)
	now := time.Date(2026, time.November, 3, 10, 15, 0, 0, loc)

	from, to := DigestWindowFor(now)
	want := time.Date(2026, time.November, 3, 0, 0, 0, 0, loc)
	if !from.Equal(want) {
		t.Errorf("from = %v, want local midnight %v", from, want)
	}
	if !to.Equal(now) {
		t.Errorf("to = %v, want %v", to, now)
	}
	if from.Location() != now.Location() {
		t.Errorf("window changed timezone: %v", from.Location())
	}
	if !from.Before(to) {
		t.Errorf("window is empty: %v to %v", from, to)
	}
}

func TestDigestHasActivity(t *testing.T) {
	tests := []struct {
		name string
		d    Digest
		want bool
	}{
		{"nothing at all", Digest{}, false},
		{"outstanding work is not activity", Digest{
			Issues:      DigestIssues{Open: 9},
			Maintenance: DigestMaintenance{DueToday: 2, Overdue: 3},
			Stock:       DigestStock{Low: 4},
			UnreadCount: 7,
		}, false},
		{"an issue logged is activity", Digest{Issues: DigestIssues{Logged: 1}}, true},
		{"a resolved issue is activity", Digest{Issues: DigestIssues{Resolved: 1}}, true},
		{"a fault check is activity", Digest{Maintenance: DigestMaintenance{FaultsFound: 1}}, true},
		{"stock used is activity", Digest{Stock: DigestStock{Used: 3}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DigestHasActivity(&tc.d); got != tc.want {
				t.Errorf("DigestHasActivity() = %v, want %v", got, tc.want)
			}
		})
	}
}
