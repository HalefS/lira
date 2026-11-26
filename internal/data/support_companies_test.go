package data

import (
	"strings"
	"testing"

	"github.com/HalefS/lira/internal/validator"
)

// The slug is the value written on every handover and the only part of a
// company that can never be edited, so what it derives from a name is worth
// pinning down: accents folded, punctuation collapsed, and never empty.
func TestSlugify(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain lower case", "telnet", "telnet"},
		{"capitalised", "Telnet", "telnet"},
		{"accented", "Telefónica", "telefonica"},
		{"spaces become one hyphen", "Acme Systems", "acme-systems"},
		{"leading and trailing spaces are dropped", "  Acme  ", "acme"},
		{"punctuation collapses", "Ação 24/7", "acao-24-7"},
		{"runs of punctuation collapse to one hyphen", "A -- B", "a-b"},
		{"other accents fold", "Zürich", "zurich"},
		{"nothing usable is empty", "!!!", ""},
		{"empty is empty", "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := slugify(c.in); got != c.want {
				t.Fatalf("slugify(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestValidateSupportCompany(t *testing.T) {
	valid := func() *SupportCompany {
		return &SupportCompany{Name: "Acme Systems", Color: "blue"}
	}

	cases := []struct {
		name  string
		apply func(*SupportCompany)
		valid bool
	}{
		{"a normal company is valid", func(c *SupportCompany) {}, true},
		{"every palette token is accepted", func(c *SupportCompany) { c.Color = "teal" }, true},
		{"a missing name is rejected", func(c *SupportCompany) { c.Name = "   " }, false},
		{"a name over 50 characters is rejected", func(c *SupportCompany) { c.Name = strings.Repeat("a", 51) }, false},
		{"an unknown colour is rejected", func(c *SupportCompany) { c.Color = "purple" }, false},
		{"an empty colour is rejected", func(c *SupportCompany) { c.Color = "" }, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			company := valid()
			c.apply(company)

			v := validator.New()
			ValidateSupportCompany(v, company)
			if got := v.Valid(); got != c.valid {
				t.Fatalf("ValidateSupportCompany(%+v) valid = %v, want %v (errors: %v)",
					company, got, c.valid, v.Errors)
			}
		})
	}
}
