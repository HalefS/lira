package data

import (
	"testing"

	"github.com/HalefS/lira/internal/validator"
)

// A catalog with one company of each kind, plus a generic company that stands
// in for anything a manager adds in Settings.
func supportTestCatalog() []*SupportCompany {
	return []*SupportCompany{
		{Slug: "telnet", Name: "Telnet", Color: "blue", FieldKind: SupportFieldTechnician, Active: true},
		{Slug: "telefonica", Name: "Telefónica", Color: "amber", FieldKind: SupportFieldTicket, Active: true},
		{Slug: "acme", Name: "Acme Systems", Color: "teal", FieldKind: SupportFieldReference, Active: true},
	}
}

func supportStr(s string) *string { return &s }

func supportUse(company string) SupportRequestUse {
	return SupportRequestUse{
		Company:   company,
		Status:    SupportStatusPending,
		StartedAt: "2026-11-10T09:00",
	}
}

// The validation rule is driven by the catalog rather than by the two built-in
// slugs, which is the point of the catalog: a company added in Settings must be
// accepted with the fields its kind actually carries, and an unknown company
// must be refused before the foreign key turns it into a 500.
func TestValidateSupportRequests(t *testing.T) {
	cases := []struct {
		name  string
		apply func(*SupportRequestUse)
		valid bool
	}{
		{"a pending Telnet handover needs its technician", func(u *SupportRequestUse) {
			u.Company = "telnet"
			u.TelnetTechnician = supportStr("Ana")
		}, true},
		{"a Telnet handover without a technician is rejected", func(u *SupportRequestUse) {
			u.Company = "telnet"
		}, false},
		{"a solved Telnet handover needs who confirmed it", func(u *SupportRequestUse) {
			u.Company = "telnet"
			u.Status = SupportStatusSolved
			u.TelnetTechnician = supportStr("Ana")
		}, false},
		{"a solved Telnet handover with a confirmation is valid", func(u *SupportRequestUse) {
			u.Company = "telnet"
			u.Status = SupportStatusSolved
			u.TelnetTechnician = supportStr("Ana")
			u.TelnetConfirmedBy = supportStr("Rui")
			u.ResolvedAt = "2026-11-10T10:30"
		}, true},
		{"a Telefónica handover needs its ticket", func(u *SupportRequestUse) {
			u.Company = "telefonica"
		}, false},
		{"a Telefónica handover with a ticket is valid", func(u *SupportRequestUse) {
			u.Company = "telefonica"
			u.TelefonicaTicketID = supportStr("2026-88123")
		}, true},
		{"a generic company needs its reference", func(u *SupportRequestUse) {
			u.Company = "acme"
		}, false},
		{"a generic company with a reference is valid", func(u *SupportRequestUse) {
			u.Company = "acme"
			u.Reference = supportStr("CASE-42")
		}, true},
		{"an unknown company is rejected", func(u *SupportRequestUse) {
			u.Company = "nobody"
			u.Reference = supportStr("X")
		}, false},
		{"a blank technician counts as missing", func(u *SupportRequestUse) {
			u.Company = "telnet"
			u.TelnetTechnician = supportStr("   ")
		}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			use := supportUse("")
			c.apply(&use)

			v := validator.New()
			ValidateSupportRequests(v, []SupportRequestUse{use}, supportTestCatalog())
			if got := v.Valid(); got != c.valid {
				t.Fatalf("ValidateSupportRequests(%+v) valid = %v, want %v (errors: %v)",
					use, got, c.valid, v.Errors)
			}
		})
	}
}

func TestValidateSupportRequestsDuplicates(t *testing.T) {
	base := func(company, reference string) SupportRequestUse {
		return SupportRequestUse{
			Company:   company,
			Status:    SupportStatusPending,
			Reference: supportStr(reference),
			StartedAt: "2026-11-10T09:00",
		}
	}
	ticket := func(id string) SupportRequestUse {
		return SupportRequestUse{
			Company:            "telefonica",
			Status:             SupportStatusPending,
			TelefonicaTicketID: supportStr(id),
			StartedAt:          "2026-11-10T09:00",
		}
	}

	cases := []struct {
		name string
		uses []SupportRequestUse
		ok   bool
	}{
		{"the same ticket twice is rejected", []SupportRequestUse{ticket("2026-1"), ticket("2026-1")}, false},
		{"the same reference in any case is rejected", []SupportRequestUse{base("acme", "CASE-1"), base("acme", "case-1")}, false},
		{"different references are accepted", []SupportRequestUse{base("acme", "CASE-1"), base("acme", "CASE-2")}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := validator.New()
			ValidateSupportRequests(v, c.uses, supportTestCatalog())
			if got := v.Valid(); got != c.ok {
				t.Fatalf("valid = %v, want %v (errors: %v)", got, c.ok, v.Errors)
			}
		})
	}
}

func TestValidateSupportRequestsLimit(t *testing.T) {
	uses := make([]SupportRequestUse, maxSupportRequestsPerIssue+1)
	for i := range uses {
		uses[i] = SupportRequestUse{
			Company:   "acme",
			Status:    SupportStatusPending,
			Reference: supportStr("CASE-" + string(rune('A'+i))),
			StartedAt: "2026-11-10T09:00",
		}
	}

	v := validator.New()
	ValidateSupportRequests(v, uses, supportTestCatalog())
	if v.Valid() {
		t.Fatalf("expected more than %d handovers to be rejected", maxSupportRequestsPerIssue)
	}
}
