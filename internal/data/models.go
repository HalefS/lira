package data

import (
	"database/sql"
	"errors"
)

var (
	ErrRecordNotFound      = errors.New("record not found")
	ErrEditConflict        = errors.New("edit conflict")
	ErrDuplicateEmail      = errors.New("duplicate email")
	ErrDuplicateIssueType  = errors.New("duplicate issue type")
	ErrDuplicateDepartment = errors.New("duplicate department")
	// A rename would move a department's name onto one that already has a
	// recurring alert of the same type, and the two cannot both exist.
	ErrDepartmentRenameClash  = errors.New("department rename would collide with an existing recurring alert")
	ErrDuplicateConnectaAgent = errors.New("duplicate connecta agent")

	// ErrDuplicateShift: a shift with this name already exists. The name is what
	// the cell picker shows and what a rota is read by, so two shifts called
	// "Night" would be indistinguishable on the grid.
	ErrDuplicateShift = errors.New("duplicate shift")

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
	Rooms           RoomModel
	ConnectaAgents  ConnectaAgentModel
	Settings        SettingsModel
	RecurringAlerts RecurringAlertModel
	Analytics       AnalyticsModel
	Consumables     ConsumableModel
	ConsumableItems ConsumableItemModel
	SupportRequests SupportRequestModel
	TVSwaps         TVSwapModel
	TVSwapAlerts    TVSwapAlertModel
	LCU             LCUModel
	Maintenance     MaintenanceModel
	Shifts          ShiftModel
	Schedule        ScheduleModel
	// Absences are a separate model from Schedule, not a method on it: the
	// rota is a recurring pattern with no dates in it, and a model holding both
	// eventually grows a method that needs a week argument it has no other use
	// for. ScheduleModel.Week reads absences itself, so both call sites get them
	// without either having to remember.
	Absences AbsenceModel
}

func NewModels(db *sql.DB) Models {
	return Models{
		Users:           UserModel{DB: db},
		Tokens:          TokenModel{DB: db},
		Issues:          IssueModel{DB: db},
		IssueTypes:      IssueTypeModel{DB: db},
		Departments:     DepartmentModel{DB: db},
		Rooms:           RoomModel{DB: db},
		ConnectaAgents:  ConnectaAgentModel{DB: db},
		Settings:        SettingsModel{DB: db},
		RecurringAlerts: RecurringAlertModel{DB: db},
		Analytics:       AnalyticsModel{DB: db},
		Consumables:     ConsumableModel{DB: db},
		ConsumableItems: ConsumableItemModel{DB: db},
		SupportRequests: SupportRequestModel{DB: db},
		TVSwaps:         TVSwapModel{DB: db},
		TVSwapAlerts:    TVSwapAlertModel{DB: db},
		LCU:             LCUModel{DB: db},
		Maintenance:     MaintenanceModel{DB: db},
		Shifts:          ShiftModel{DB: db},
		Schedule:        ScheduleModel{DB: db},
		Absences:        AbsenceModel{DB: db},
	}
}
