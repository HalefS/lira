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
)

// The fields a handover to a company carries. The two built-ins keep the shape
// they always had; every company added later uses the generic reference.
const (
	SupportFieldTechnician = "technician"
	SupportFieldTicket     = "ticket"
	SupportFieldReference  = "reference"
)

// supportCompanyColors are the palette tokens the UI already has classes for.
// Kept here rather than accepting a hex value so a company's colour follows the
// theme into dark mode instead of being frozen at whatever was picked.
var supportCompanyColors = map[string]bool{
	"blue": true, "teal": true, "amber": true,
	"green": true, "red": true, "gray": true,
}

// SupportCompany is one external company an issue can be handed over to.
//
// Slug is the stable key written on every handover. Name is what people read, so
// a rename is safe: it changes this row and nothing else, and every screen joins
// through here.
type SupportCompany struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	FieldKind string    `json:"field_kind"`
	Active    bool      `json:"active"`
	CreatedBy *int64    `json:"created_by,omitempty"`
}

func ValidateSupportCompany(v *validator.Validator, c *SupportCompany) {
	name := strings.TrimSpace(c.Name)
	v.Check(name != "", "name", "must be provided")
	v.Check(len(name) <= 50, "name", "must not be more than 50 characters")
	v.Check(supportCompanyColors[c.Color], "color", "must be one of blue, teal, amber, green, red or gray")
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
		companies = append(companies, &c)
	}
	return companies, rows.Err()
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
	return &c, nil
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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var exists bool
	if err := m.DB.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM support_companies WHERE name = $1)`, c.Name).
		Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrDuplicateSupportCompany
	}

	slug, err := m.uniqueSlug(ctx, c.Name)
	if err != nil {
		return err
	}
	c.Slug = slug

	query := `
		INSERT INTO support_companies (slug, name, color, field_kind, active, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`

	err = m.DB.QueryRowContext(ctx, query,
		c.Slug, c.Name, c.Color, c.FieldKind, c.Active, c.CreatedBy,
	).Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		if isDuplicateSupportCompanyName(err) {
			return ErrDuplicateSupportCompany
		}
		return err
	}
	return nil
}

// uniqueSlug finds the first free slug for a name. The loop is bounded only by
// the number of companies, which is a short list, so this is not a hot path.
func (m SupportCompanyModel) uniqueSlug(ctx context.Context, name string) (string, error) {
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
		if err := m.DB.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM support_companies WHERE slug = $1)`, candidate).
			Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
}

// Update saves the editable parts of a company: its display name, colour and
// whether it is still offered. The slug is deliberately not editable.
func (m SupportCompanyModel) Update(c *SupportCompany) error {
	c.Name = strings.TrimSpace(c.Name)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := m.DB.ExecContext(ctx, `
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
	return nil
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
