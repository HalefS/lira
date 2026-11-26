package data

import (
	"testing"

	"github.com/HalefS/lira/internal/validator"
)

// The catalog is configuration, including the fields each company asks for.
// These three stand in for Telnet, Telefónica and a company added in Settings.
func supportTestCatalog() []*SupportCompany {
	return []*SupportCompany{
		{
			Slug: "telnet", Name: "Telnet", Color: "blue", Active: true,
			Fields: []*SupportCompanyField{
				{Key: "technician", Label: "Reported to", Type: SupportFieldName, Required: true, Active: true},
				{Key: "confirmed_by", Label: "Reported fixed by", Type: SupportFieldConfirmation, Active: true},
			},
		},
		{
			Slug: "telefonica", Name: "Telefónica", Color: "amber", Active: true,
			Fields: []*SupportCompanyField{
				{Key: "ticket_id", Label: "Ticket ID", Type: SupportFieldReference, Required: true, Active: true},
			},
		},
		{
			Slug: "acme", Name: "Acme Systems", Color: "teal", Active: true,
			Fields: []*SupportCompanyField{
				{Key: "reference", Label: "Case number", Type: SupportFieldReference, Required: true, Active: true},
				{Key: "agent", Label: "Agent name", Type: SupportFieldName, Active: true},
			},
		},
	}
}

// supportFields pairs keys and values so a test reads as the form does.
func supportFields(pairs ...string) []SupportRequestFieldUse {
	out := make([]SupportRequestFieldUse, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, SupportRequestFieldUse{Key: pairs[i], Value: pairs[i+1]})
	}
	return out
}

func supportUse(company string) SupportRequestUse {
	return SupportRequestUse{
		Company:   company,
		Status:    SupportStatusPending,
		StartedAt: "2026-11-10T09:00",
	}
}

// Required comes from each company's field setup rather than from the slug.
func TestValidateSupportRequests(t *testing.T) {
	cases := []struct {
		name  string
		apply func(*SupportRequestUse)
		valid bool
	}{
		{"a pending Telnet handover needs its technician", func(u *SupportRequestUse) {
			u.Company = "telnet"
			u.Fields = supportFields("technician", "Ana")
		}, true},
		{"a Telnet handover without a technician is rejected", func(u *SupportRequestUse) {
			u.Company = "telnet"
		}, false},
		{"a solved Telnet handover needs who confirmed it", func(u *SupportRequestUse) {
			u.Company = "telnet"
			u.Status = SupportStatusSolved
			u.Fields = supportFields("technician", "Ana")
		}, false},
		{"a solved Telnet handover with a confirmation is valid", func(u *SupportRequestUse) {
			u.Company = "telnet"
			u.Status = SupportStatusSolved
			u.Fields = supportFields("technician", "Ana", "confirmed_by", "Rui")
			u.ResolvedAt = "2026-11-10T10:30"
		}, true},
		{"a Telefónica handover needs its ticket", func(u *SupportRequestUse) {
			u.Company = "telefonica"
		}, false},
		{"a Telefónica handover with a ticket is valid", func(u *SupportRequestUse) {
			u.Company = "telefonica"
			u.Fields = supportFields("ticket_id", "2026-88123")
		}, true},
		{"a company's optional field may be left blank", func(u *SupportRequestUse) {
			u.Company = "acme"
			u.Fields = supportFields("reference", "CASE-42")
		}, true},
		{"an optional field with a value is valid", func(u *SupportRequestUse) {
			u.Company = "acme"
			u.Fields = supportFields("reference", "CASE-42", "agent", "Maria")
		}, true},
		{"an unknown company is rejected", func(u *SupportRequestUse) {
			u.Company = "nobody"
			u.Fields = supportFields("reference", "X")
		}, false},
		{"a blank required value counts as missing", func(u *SupportRequestUse) {
			u.Company = "acme"
			u.Fields = supportFields("reference", "   ")
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
	base := func(reference string) SupportRequestUse {
		u := supportUse("acme")
		u.Fields = supportFields("reference", reference)
		return u
	}
	ticket := func(id string) SupportRequestUse {
		u := supportUse("telefonica")
		u.Fields = supportFields("ticket_id", id)
		return u
	}

	cases := []struct {
		name string
		uses []SupportRequestUse
		ok   bool
	}{
		{"the same ticket twice is rejected", []SupportRequestUse{ticket("2026-1"), ticket("2026-1")}, false},
		{"the same reference in any case is rejected", []SupportRequestUse{base("CASE-1"), base("case-1")}, false},
		{"different references are accepted", []SupportRequestUse{base("CASE-1"), base("CASE-2")}, true},
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

func TestValidateSupportRequestsFieldTypes(t *testing.T) {
	catalog := []*SupportCompany{{
		Slug: "beta", Name: "Beta Systems", Active: true,
		Fields: []*SupportCompanyField{
			{Key: "expected", Label: "Expected date", Type: SupportFieldDate, Required: true, Active: true},
			{Key: "count", Label: "Units", Type: SupportFieldNumber, Active: true},
		},
	}}

	cases := []struct {
		name   string
		fields []SupportRequestFieldUse
		valid  bool
	}{
		{"a date field takes a date", supportFields("expected", "2026-11-10"), true},
		{"a date field refuses free text", supportFields("expected", "tomorrow"), false},
		{"a number field takes a number", supportFields("expected", "2026-11-10", "count", "12.5"), true},
		{"a number field refuses words", supportFields("expected", "2026-11-10", "count", "twelve"), false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := supportUse("beta")
			u.Fields = c.fields
			v := validator.New()
			ValidateSupportRequests(v, []SupportRequestUse{u}, catalog)
			if got := v.Valid(); got != c.valid {
				t.Fatalf("valid = %v, want %v (errors: %v)", got, c.valid, v.Errors)
			}
		})
	}
}

func TestValidateSupportRequestsLimit(t *testing.T) {
	uses := make([]SupportRequestUse, maxSupportRequestsPerIssue+1)
	for i := range uses {
		uses[i] = supportUse("acme")
		uses[i].Fields = supportFields("reference", "CASE-"+string(rune('A'+i)))
	}

	v := validator.New()
	ValidateSupportRequests(v, uses, supportTestCatalog())
	if v.Valid() {
		t.Fatalf("expected more than %d handovers to be rejected", maxSupportRequestsPerIssue)
	}
}
