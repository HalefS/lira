package data

import (
	"database/sql"
	"errors"
)

var (
	ErrRecordNotFound     = errors.New("record not found")
	ErrEditConflict       = errors.New("edit conflict")
	ErrDuplicateEmail     = errors.New("duplicate email")
	ErrDuplicateIssueType = errors.New("duplicate issue type")
)

type Models struct {
	Users      UserModel
	Tokens     TokenModel
	Issues     IssueModel
	IssueTypes IssueTypeModel
	Settings   SettingsModel
}

func NewModels(db *sql.DB) Models {
	return Models{
		Users:      UserModel{DB: db},
		Tokens:     TokenModel{DB: db},
		Issues:     IssueModel{DB: db},
		IssueTypes: IssueTypeModel{DB: db},
		Settings:   SettingsModel{DB: db},
	}
}
