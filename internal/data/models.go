package data

import (
	"database/sql"
	"errors"
)

var (
	ErrRecordNotFound         = errors.New("record not found")
	ErrEditConflict           = errors.New("edit conflict")
	ErrDuplicateEmail         = errors.New("duplicate email")
	ErrDuplicateIssueType     = errors.New("duplicate issue type")
	ErrDuplicateDepartment    = errors.New("duplicate department")
	ErrDuplicateConnectaAgent = errors.New("duplicate connecta agent")

	ErrDuplicateConsumableItem = errors.New("duplicate consumable item")
	// ErrDuplicateSupportTicket: this Telefónica ticket id is already logged
	// against this issue.
	ErrDuplicateSupportTicket = errors.New("duplicate third-party support ticket")
)

type Models struct {
	Users           UserModel
	Tokens          TokenModel
	Issues          IssueModel
	IssueTypes      IssueTypeModel
	Departments     DepartmentModel
	ConnectaAgents  ConnectaAgentModel
	Settings        SettingsModel
	RecurringAlerts RecurringAlertModel
	Analytics       AnalyticsModel
	Consumables     ConsumableModel
	ConsumableItems ConsumableItemModel
	SupportRequests SupportRequestModel
}

func NewModels(db *sql.DB) Models {
	return Models{
		Users:           UserModel{DB: db},
		Tokens:          TokenModel{DB: db},
		Issues:          IssueModel{DB: db},
		IssueTypes:      IssueTypeModel{DB: db},
		Departments:     DepartmentModel{DB: db},
		ConnectaAgents:  ConnectaAgentModel{DB: db},
		Settings:        SettingsModel{DB: db},
		RecurringAlerts: RecurringAlertModel{DB: db},
		Analytics:       AnalyticsModel{DB: db},
		Consumables:     ConsumableModel{DB: db},
		ConsumableItems: ConsumableItemModel{DB: db},
		SupportRequests: SupportRequestModel{DB: db},
	}
}
