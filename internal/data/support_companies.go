package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/HalefS/lira/internal/validator"
	"github.com/lib/pq"
)

// The kinds of information a manager can ask a company for on a handover.
// These are input types, not company kinds: Telnet can have a name and a
// confirmation, Telefónica a ticket, and a new company any mix of them.
const (
	SupportFieldText         = "text"
	SupportFieldName         = "name"
	SupportFieldConfirmation = "confirmation"
	SupportFieldReference    = "reference"
	SupportFieldDate         = "date"
	SupportFieldNumber       = "number"
)

const (
	maxSupportFieldsPerCompany = 8
	maxSupportFieldLabelLength = 60
)

// SupportFieldReference is kept as the company's default field_kind for rows
// created before field configuration; field definitions are now the authority.

var supportCompanyColors = map[string]bool{
	"blue": true, "teal": true, "amber": true,
	"green": true, "red": true, "gray": true,
}

var supportFieldTypes = map[string]bool{
	SupportFieldText:         true,
	SupportFieldName:         true,
	SupportFieldConfirmation: true,
	SupportFieldReference:    true,
	SupportFieldDate:         true,
	SupportFieldNumber:       true,
}

// SupportCompanyField is one piece of information a handover to a company
// asks for. The key is stable so a label can be edited without orphaning the
// values already recorded against it.
type SupportCompanyField struct {
	ID       int64  `json:"id,omitempty"`
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Position int    `json:"position,omitempty"`
	Active   bool   `json:"active"`
}

// SupportCompany is one external company an issue can be handed over to.
//
// Slug is the stable key written on every handover. Name is what people read, so
// a rename is safe: it changes this row and nothing else, and every screen joins
// through here. Fields are the company's handover form as configured in
// Settings; inactive ones are still returned so old handovers can show them.
type SupportCompany struct {
	ID        int64                  `json:"id"`
	CreatedAt time.Time              `json:"created_at"`
	Slug      string                 `json:"slug"`
	Name      string                 `json:"name"`
	Color     string                 `json:"color"`
	FieldKind string                 `json:"field_kind"`
	Active    bool                   `json:"active"`
	CreatedBy *int64                 `json:"created_by,omitempty"`
	Fields    []*SupportCompanyField `json:"fields"`
}

// DefaultSupportCompanyFields gives a company added in Settings the same
// single reference it had before fields became configurable, so a manager who
// adds a company and does not touch its setup sees the old behaviour.
func DefaultSupportCompanyFields() []*SupportCompanyField {
	return []*SupportCompanyField{{
		Key: "reference", Label: "Reference", Type: SupportFieldReference,
		Required: true, Active: true, Position: 1,
	}}
}

func ValidateSupportCompany(v *validator.Validator, c *SupportCompany) {
	name := strings.TrimSpace(c.Name)
	v.Check(name != "", "name", "must be provided")
	v.Check(len(name) <= 50, "name", "must not be more than 50 characters")
	v.Check(supportCompanyColors[c.Color], "color", "must be one of blue, teal, amber, green, red or gray")
	v.Check(len(c.Fields) <= maxSupportFieldsPerCompany, "fields",
		fmt.Sprintf("must not contain more than %d fields", maxSupportFieldsPerCompany))

	seen := make(map[string]bool, len(c.Fields))
	references := 0
	for i, f := range c.Fields {
		f.Label = strings.TrimSpace(f.Label)
		f.Key = strings.TrimSpace(f.Key)
		if f.Key == "" {
			f.Key = slugify(f.Label)
			if f.Key == "" {
				f.Key = fmt.Sprintf("field-%d", i+1)
			}
		}
		f.Position = i + 1
		f.Active = true

		v.Check(f.Label != "", "fields", "every field needs a label")
		v.Check(len(f.Label) <= maxSupportFieldLabelLength, "fields",
			fmt.Sprintf("field labels must not be more than %d characters", maxSupportFieldLabelLength))
		v.Check(supportFieldTypes[f.Type], "fields",
			"field types must be text, name, confirmation, reference, date or number")
		if seen[f.Key] {
			v.AddError("fields", "the same field key cannot be used twice on one company")
		}
		seen[f.Key] = true
		if f.Type == SupportFieldReference {
			references++
		}
	}
	// One tracking reference is the one the duplicate rule uses. A second would
	// make it ambiguous which number the rule should enforce.
	v.Check(references <= 1, "fields", "a company can have at most one reference field")
}

type supportQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type SupportCompanyModel struct {
	DB *sql.DB
}

func (m SupportCompanyModel) GetAll() ([]*SupportCompany, error) {
	query := `
		SELECT id, created_at, slug, name, color, field_kind, active, created_by
		FROM support_companies
		ORDER BY name ASC`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var companies []*SupportCompany
	bySlug := make(map[string]*SupportCompany)
	for rows.Next() {
		var c SupportCompany
		var createdBy sql.NullInt64
		if err := rows.Scan(&c.ID, &c.CreatedAt, &c.Slug, &c.Name, &c.Color,
			&c.FieldKind, &c.Active, &createdBy); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			id := createdBy.Int64
			c.CreatedBy = &id
		}
		c.Fields = []*SupportCompanyField{}
		companies = append(companies, &c)
		bySlug[c.Slug] = &c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(companies) == 0 {
		return companies, nil
	}

	slugs := make([]string, 0, len(bySlug))
	for slug := range bySlug {
		slugs = append(slugs, slug)
	}
	if err := loadSupportCompanyFields(ctx, m.DB, slugs, bySlug); err != nil {
		return nil, err
	}
	return companies, nil
}

func (m SupportCompanyModel) Get(id int64) (*SupportCompany, error) {
	query := `
		SELECT id, created_at, slug, name, color, field_kind, active, created_by
		FROM support_companies
		WHERE id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var c SupportCompany
	var createdBy sql.NullInt64
	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&c.ID, &c.CreatedAt, &c.Slug, &c.Name, &c.Color,
		&c.FieldKind, &c.Active, &createdBy,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	if createdBy.Valid {
		createdByID := createdBy.Int64
		c.CreatedBy = &createdByID
	}
	c.Fields = []*SupportCompanyField{}
	if err := loadSupportCompanyFields(ctx, m.DB, []string{c.Slug},
		map[string]*SupportCompany{c.Slug: &c}); err != nil {
		return nil, err
	}
	return &c, nil
}

// loadSupportCompanyFields attaches every definition, active or not, so a
// handover can still show the value of a field a manager later removed.
func loadSupportCompanyFields(ctx context.Context, q supportQuerier, slugs []string, bySlug map[string]*SupportCompany) error {
	if len(slugs) == 0 {
		return nil
	}
	rows, err := q.QueryContext(ctx, `
		SELECT id, company_slug, field_key, label, field_type, required, active, position
		FROM support_company_fields
		WHERE company_slug = ANY($1::text[])
		ORDER BY position, id`, pq.Array(slugs))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var f SupportCompanyField
		var slug string
		if err := rows.Scan(&f.ID, &slug, &f.Key, &f.Label, &f.Type,
			&f.Required, &f.Active, &f.Position); err != nil {
			return err
		}
		if c, ok := bySlug[slug]; ok {
			c.Fields = append(c.Fields, &f)
		}
	}
	return rows.Err()
}

// Insert adds a company, deriving a unique slug from its name.
//
// The slug is generated rather than entered: it is an implementation key that
// ends up on every handover, so asking a manager to type one would be asking
// them to make a permanent decision about data they cannot see. A collision is
// resolved with a numeric suffix, which keeps the slug stable from then on.
func (m SupportCompanyModel) Insert(c *SupportCompany) error {
	c.Name = strings.TrimSpace(c.Name)
	if c.FieldKind == "" {
		c.FieldKind = SupportFieldReference
	}
	if c.Color == "" {
		c.Color = "blue"
	}
	if c.Fields == nil {
		c.Fields = DefaultSupportCompanyFields()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM support_companies WHERE name = $1)`, c.Name).
		Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrDuplicateSupportCompany
	}

	slug, err := uniqueSlug(ctx, tx, c.Name)
	if err != nil {
		return err
	}
	c.Slug = slug

	query := `
		INSERT INTO support_companies (slug, name, color, field_kind, active, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`

	err = tx.QueryRowContext(ctx, query,
		c.Slug, c.Name, c.Color, c.FieldKind, c.Active, c.CreatedBy,
	).Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		if isDuplicateSupportCompanyName(err) {
			return ErrDuplicateSupportCompany
		}
		return err
	}

	if err := replaceSupportCompanyFields(ctx, tx, c.Slug, c.Fields); err != nil {
		return err
	}
	return tx.Commit()
}

// uniqueSlug finds the first free slug for a name. The loop is bounded only by
// the number of companies, which is a short list, so this is not a hot path.
func uniqueSlug(ctx context.Context, q supportQuerier, name string) (string, error) {
	base := slugify(name)
	if base == "" {
		base = "company"
	}
	for i := 1; ; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}
		var exists bool
		if err := q.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM support_companies WHERE slug = $1)`, candidate).
			Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
}

// replaceSupportCompanyFields applies the manager's field setup. Definitions
// left out of the list are deactivated, never deleted: old handovers still join
// to them and keep showing their recorded values.
func replaceSupportCompanyFields(ctx context.Context, q supportQuerier, slug string, fields []*SupportCompanyField) error {
	if _, err := q.ExecContext(ctx, `
		UPDATE support_company_fields SET active = false WHERE company_slug = $1`, slug); err != nil {
		return err
	}
	for _, f := range fields {
		if f.Key == "" {
			f.Key = slugify(f.Label)
			if f.Key == "" {
				f.Key = "field"
			}
		}
		f.Active = true
		err := q.QueryRowContext(ctx, `
			INSERT INTO support_company_fields
				(company_slug, field_key, label, field_type, required, active, position)
			VALUES ($1, $2, $3, $4, $5, true, $6)
			ON CONFLICT (company_slug, field_key) DO UPDATE
			SET label = EXCLUDED.label, field_type = EXCLUDED.field_type,
			    required = EXCLUDED.required, active = true,
			    position = EXCLUDED.position
			RETURNING id`,
			slug, f.Key, f.Label, f.Type, f.Required, f.Position,
		).Scan(&f.ID)
		if err != nil {
			return err
		}
	}
	return nil
}

// Update saves the editable parts of a company and, when Fields is non-nil,
// its field setup. The slug is deliberately not editable, because it is what
// existing handovers reference.
func (m SupportCompanyModel) Update(c *SupportCompany) error {
	c.Name = strings.TrimSpace(c.Name)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		UPDATE support_companies
		SET name = $1, color = $2, active = $3
		WHERE id = $4`,
		c.Name, c.Color, c.Active, c.ID)
	if err != nil {
		if isDuplicateSupportCompanyName(err) {
			return ErrDuplicateSupportCompany
		}
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrRecordNotFound
	}
	if c.Fields != nil {
		if err := replaceSupportCompanyFields(ctx, tx, c.Slug, c.Fields); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func isDuplicateSupportCompanyName(err error) bool {
	return err != nil &&
		strings.Contains(err.Error(), `duplicate key value violates unique constraint "support_companies_name_key"`)
}

// slugify turns a display name into a stable key: lower case, accents folded,
// anything that is not a letter or digit collapsed to a single hyphen.
func slugify(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(name) {
		r = foldAccent(unicode.ToLower(r))
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash && b.Len() > 0:
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// foldAccent maps the accented letters the app's two languages actually use to
// their ASCII form, so "Telefónica" and "Ação" become usable slugs without
// pulling in a full Unicode transliteration table.
func foldAccent(r rune) rune {
	switch r {
	case 'á', 'à', 'â', 'ã', 'ä', 'å':
		return 'a'
	case 'é', 'è', 'ê', 'ë':
		return 'e'
	case 'í', 'ì', 'î', 'ï':
		return 'i'
	case 'ó', 'ò', 'ô', 'õ', 'ö':
		return 'o'
	case 'ú', 'ù', 'û', 'ü':
		return 'u'
	case 'ç':
		return 'c'
	case 'ñ':
		return 'n'
	default:
		return r
	}
}
