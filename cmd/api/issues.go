package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// normaliseClockTime drops a blank submitted time, so an issue with no time at
// all stores NULL rather than an empty string. A Pending issue has no end time
// -- the job has not been fixed, so it has not ended.
//
// Values may be "HH:MM" (legacy) or "YYYY-MM-DDTHH:MM" (dated). Both are kept
// as-is; the timestamp form is parsed server-side.
func normaliseClockTime(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	return v
}

// applyClockTime copies a submitted clock time onto an issue being updated, and
// has to tell three cases apart that would otherwise be indistinguishable:
//
//	nil      the client did not mention the field, so the stored value stands
//	""                    an explicit "there is no time here", which clears the column
//	"HH:MM"               a legacy clock-only value, which replaces it
//	"YYYY-MM-DDTHH:MM"    a dated value, which replaces it
//
// Without the middle case, switching a resolved issue to Pending would leave its
// old end time on the row, so the issue would read as ended while its status
// says it is still waiting.
func applyClockTime(dst **string, submitted *string) {
	switch {
	case submitted == nil:
		// not mentioned -- leave whatever is already stored
	case strings.TrimSpace(*submitted) == "":
		*dst = nil
	default:
		*dst = submitted
	}
}

func (app *application) listIssuesHandler(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()

	filters := data.IssueFilters{
		Mode:   app.readString(qs, "mode", ""),
		Status: app.readString(qs, "status", ""),
		Type:   app.readString(qs, "type", ""),
		Search: app.readString(qs, "search", ""),
		Date:   app.readString(qs, "date", ""),
	}

	limit, capped := app.readLimit(r)

	issues, total, err := app.models.Issues.GetAll(filters, limit)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if issues == nil {
		// Always an array: the tables map over this without a nil check, and a
		// null here reads as a broken response rather than an empty list.
		issues = []*data.Issue{}
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"issues":       issues,
		"total":        total,
		"limit":        limit,
		"limit_capped": capped,
	}, nil)
}

// checkDuplicateIssuesHandler powers the recurring-issue alert shown while
// logging an issue: it looks for other issues of the same type at the same
// location within the manager-configured time window.
func (app *application) checkDuplicateIssuesHandler(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	mode := app.readString(qs, "mode", "")
	location := app.readString(qs, "location", "")
	issueType := app.readString(qs, "type", "")
	excludeID := int64(app.readInt(qs, "exclude_id", 0))

	v := validator.New()
	v.Check(mode == "apt" || mode == "dept", "mode", "must be 'apt' or 'dept'")
	v.Check(location != "", "location", "must be provided")
	v.Check(issueType != "", "type", "must be provided")
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	settings, err := app.models.Settings.Get()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	duplicates, err := app.models.Issues.GetRecentDuplicates(mode, location, issueType, settings.DuplicateWindowHours, excludeID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if duplicates == nil {
		duplicates = []*data.DuplicateIssue{}
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"count":        len(duplicates),
		"window_hours": settings.DuplicateWindowHours,
		"issues":       duplicates,
	}, nil)
}

func (app *application) createIssueHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Mode             string  `json:"mode"`
		Location         string  `json:"location"`
		Type             string  `json:"type"`
		Problem          string  `json:"problem"`
		Resolution       string  `json:"resolution"`
		TimeMinutes      int     `json:"time_minutes"`
		Status           string  `json:"status"`
		StartTime        *string `json:"start_time"`
		EndTime          *string `json:"end_time"`
		ReportedByAgent  *string `json:"reported_by_agent"`
		ConfirmedByAgent *string `json:"confirmed_by_agent"`
		FalsePositive    bool    `json:"false_positive"`
		// The consumables used on the issue. Omitted (nil) means "leave as
		// is"; an empty list clears them.
		Consumables *[]data.ConsumableUse `json:"consumables"`
		// The handovers of the issue to Telnet / Telefonica. Same nil-means-
		// leave-as-is rule as the consumables.
		SupportRequests *[]data.SupportRequestUse `json:"support_requests"`
		// TVs moved between rooms while fixing the issue. Same nil-means-
		// leave-as-is rule.
		TVSwaps *[]data.TVSwapUse `json:"tv_swaps"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	user := app.contextGetUser(r)

	issue := &data.Issue{
		Mode:             input.Mode,
		Location:         input.Location,
		Type:             input.Type,
		Problem:          input.Problem,
		Resolution:       input.Resolution,
		TimeMinutes:      input.TimeMinutes,
		StartTime:        normaliseClockTime(input.StartTime),
		EndTime:          normaliseClockTime(input.EndTime),
		ReportedByAgent:  input.ReportedByAgent,
		ConfirmedByAgent: input.ConfirmedByAgent,
		Status:           input.Status,
		FalsePositive:    input.FalsePositive,
		LoggedBy:         user.ID,
	}

	if issue.Status == "" {
		issue.Status = "Pending"
	}

	canonicalType, err := app.resolveIssueType(issue.Type, "")
	if err != nil && !errors.Is(err, data.ErrRecordNotFound) {
		app.serverErrorResponse(w, r, err)
		return
	}

	v := validator.New()
	if errors.Is(err, data.ErrRecordNotFound) {
		v.AddError("type", "must be a valid issue type")
	} else {
		issue.Type = canonicalType
	}

	if issue.Mode == "dept" {
		canonicalDept, err := app.resolveDepartment(issue.Location, "")
		if err != nil && !errors.Is(err, data.ErrRecordNotFound) {
			app.serverErrorResponse(w, r, err)
			return
		}
		if errors.Is(err, data.ErrRecordNotFound) {
			v.AddError("location", "must be a valid department")
		} else {
			issue.Location = canonicalDept
		}
	}

	// An apartment issue's room is checked against the inventory rather than
	// accepted as typed. Nothing is allowed through on create -- allowCurrent is
	// zero, so there is no stored room to fall back on.
	//
	// The check is on the number rather than on the string, so " 1214 " and "1214"
	// cannot become two different rooms, and the value written back is the
	// canonical digits with no whitespace in them.
	if issue.Mode == "apt" {
		canonicalRoom, err := app.resolveRoom(issue.Location, 0)
		if err != nil {
			v.AddError("location", "must be a room number from the inventory")
		} else {
			issue.Location = strconv.Itoa(canonicalRoom)
		}
	}

	if resolved, err := app.resolveOptionalAgentField(v, "reported_by_agent", issue.ReportedByAgent, ""); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	} else {
		issue.ReportedByAgent = resolved
	}
	if resolved, err := app.resolveOptionalAgentField(v, "confirmed_by_agent", issue.ConfirmedByAgent, ""); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	} else {
		issue.ConfirmedByAgent = resolved
	}

	// The stored duration is always derived from the start/end clock times
	// when both are supplied, rather than trusted verbatim from the
	// client — this guarantees time_minutes stays consistent with the
	// times shown when the issue is later reopened for editing.
	baseDate := issue.CreatedAt
	if baseDate.IsZero() {
		baseDate = time.Now()
	}
	if issue.StartTime != nil {
		s := strings.TrimSpace(*issue.StartTime)
		if s == "" {
			issue.StartTime = nil
			issue.StartedAt = nil
		} else {
			data.ValidateIssueTime(v, "start_time", s)
			if v.Valid() {
				t, ok := data.ParseIssueTimeWithBase(s, baseDate)
				if !ok {
					v.AddError("start_time", "must be a time, or a date and time, in 24-hour format")
				} else {
					issue.StartedAt = &t
				}
			}
		}
	}
	if issue.EndTime != nil {
		s := strings.TrimSpace(*issue.EndTime)
		if s == "" {
			issue.EndTime = nil
			issue.ResolvedAt = nil
		} else {
			data.ValidateIssueTime(v, "end_time", s)
			if v.Valid() {
				t, ok := data.ParseIssueTimeWithBase(s, baseDate)
				if !ok {
					v.AddError("end_time", "must be a time, or a date and time, in 24-hour format")
				} else {
					issue.ResolvedAt = &t
				}
			}
		}
	}
	if issue.StartedAt != nil && issue.ResolvedAt != nil {
		if !issue.ResolvedAt.After(*issue.StartedAt) {
			v.AddError("end_time", "must be after the start time")
		} else {
			issue.TimeMinutes = int(issue.ResolvedAt.Sub(*issue.StartedAt).Minutes())
		}
	}

	if input.Consumables != nil {
		if err := app.validateConsumableUses(v, *input.Consumables); err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}
	}

	if input.TVSwaps != nil {
		app.validateTVSwaps(v, *input.TVSwaps)
	}

	var supportRequests []data.SupportRequestRecord
	if input.SupportRequests != nil {
		supportRequests, err = app.validateSupportRequests(v, *input.SupportRequests)
		if err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}
	}

	if data.ValidateIssue(v, issue); !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.Issues.Insert(issue); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	if input.Consumables != nil {
		if err := app.models.Consumables.SetForIssue(issue.ID, *input.Consumables, user.ID); err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}
	}

	if input.TVSwaps != nil {
		if err := app.setIssueTVSwaps(w, r, issue.ID, *input.TVSwaps, user.ID); err != nil {
			return
		}
	}

	if input.SupportRequests != nil {
		if err := app.setIssueSupportRequests(w, r, issue.ID, supportRequests, user.ID); err != nil {
			return
		}
	}

	// return issue with user info populated
	fullIssue, err := app.models.Issues.Get(issue.ID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusCreated, envelope{"issue": fullIssue}, nil)
}

func (app *application) showIssueHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	issue, err := app.models.Issues.Get(id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"issue": issue}, nil)
}

func (app *application) updateIssueHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	issue, err := app.models.Issues.Get(id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	originalType := issue.Type
	originalLocation := issue.Location
	originalReportedByAgent := ""
	if issue.ReportedByAgent != nil {
		originalReportedByAgent = *issue.ReportedByAgent
	}
	originalConfirmedByAgent := ""
	if issue.ConfirmedByAgent != nil {
		originalConfirmedByAgent = *issue.ConfirmedByAgent
	}

	// Permission: only the owner or a manager can edit
	currentUser := app.contextGetUser(r)
	if currentUser.Role != "manager" && issue.LoggedBy != currentUser.ID {
		app.notPermittedResponse(w, r)
		return
	}

	var input struct {
		Mode             *string `json:"mode"`
		Location         *string `json:"location"`
		Type             *string `json:"type"`
		Problem          *string `json:"problem"`
		Resolution       *string `json:"resolution"`
		TimeMinutes      *int    `json:"time_minutes"`
		Status           *string `json:"status"`
		StartTime        *string `json:"start_time"`
		EndTime          *string `json:"end_time"`
		ReportedByAgent  *string `json:"reported_by_agent"`
		ConfirmedByAgent *string `json:"confirmed_by_agent"`
		FalsePositive    *bool   `json:"false_positive"`
		// The consumables used on the issue. Omitted (nil) means "leave as
		// is"; an empty list clears them.
		Consumables *[]data.ConsumableUse `json:"consumables"`
		// The handovers of the issue to Telnet / Telefonica. Same nil-means-
		// leave-as-is rule as the consumables.
		SupportRequests *[]data.SupportRequestUse `json:"support_requests"`
		// TVs moved between rooms while fixing the issue. Same nil-means-
		// leave-as-is rule.
		TVSwaps *[]data.TVSwapUse `json:"tv_swaps"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	if input.Mode != nil {
		issue.Mode = *input.Mode
	}
	if input.Location != nil {
		issue.Location = *input.Location
	}
	if input.Type != nil {
		issue.Type = *input.Type
	}
	if input.Problem != nil {
		issue.Problem = *input.Problem
	}
	if input.Resolution != nil {
		issue.Resolution = *input.Resolution
	}
	if input.TimeMinutes != nil {
		issue.TimeMinutes = *input.TimeMinutes
	}
	if input.Status != nil {
		issue.Status = *input.Status
	}
	if input.FalsePositive != nil {
		issue.FalsePositive = *input.FalsePositive
	}
	if input.StartTime != nil {
		applyClockTime(&issue.StartTime, input.StartTime)
		// Keep the canonical timestamp in sync with the text field. A blank
		// clears it; a value parses it; an absent field (nil) leaves it alone,
		// which is what applyClockTime already decided above.
		if input.StartTime == nil {
			// not mentioned -- leave StartedAt as it is
		} else if strings.TrimSpace(*input.StartTime) == "" {
			issue.StartedAt = nil
		} else {
			baseDate := issue.CreatedAt
			if baseDate.IsZero() {
				baseDate = time.Now()
			}
			if t, ok := data.ParseIssueTimeWithBase(*input.StartTime, baseDate); ok {
				issue.StartedAt = &t
			}
		}
	}
	if input.EndTime != nil {
		applyClockTime(&issue.EndTime, input.EndTime)
		if input.EndTime == nil {
			// not mentioned -- leave ResolvedAt as it is
		} else if strings.TrimSpace(*input.EndTime) == "" {
			issue.ResolvedAt = nil
		} else {
			baseDate := issue.CreatedAt
			if baseDate.IsZero() {
				baseDate = time.Now()
			}
			if t, ok := data.ParseIssueTimeWithBase(*input.EndTime, baseDate); ok {
				issue.ResolvedAt = &t
			}
		}
	}
	if input.ReportedByAgent != nil {
		issue.ReportedByAgent = input.ReportedByAgent
	}
	if input.ConfirmedByAgent != nil {
		issue.ConfirmedByAgent = input.ConfirmedByAgent
	}

	canonicalType, err := app.resolveIssueType(issue.Type, originalType)
	if err != nil && !errors.Is(err, data.ErrRecordNotFound) {
		app.serverErrorResponse(w, r, err)
		return
	}

	v := validator.New()
	if errors.Is(err, data.ErrRecordNotFound) {
		v.AddError("type", "must be a valid issue type")
	} else {
		issue.Type = canonicalType
	}

	if issue.Mode == "dept" {
		canonicalDept, err := app.resolveDepartment(issue.Location, originalLocation)
		if err != nil && !errors.Is(err, data.ErrRecordNotFound) {
			app.serverErrorResponse(w, r, err)
			return
		}
		if errors.Is(err, data.ErrRecordNotFound) {
			v.AddError("location", "must be a valid department")
		} else {
			issue.Location = canonicalDept
		}
	}

	// Same rule as on create, with one exception: the room already on the issue is
	// allowed through even if it is no longer in the inventory. That is the point
	// at which an old record and a current catalog disagree, and refusing to edit
	// the record would be the worse answer -- it would make a range deleted by
	// mistake permanently un-editable along with every issue filed in it.
	//
	// originalLocation is read as a number, so an issue moved from a department
	// to an apartment has no stored room to fall back on and has to be given a
	// real one.
	if issue.Mode == "apt" {
		currentRoom, _ := strconv.Atoi(strings.TrimSpace(originalLocation))
		canonicalRoom, err := app.resolveRoom(issue.Location, currentRoom)
		if err != nil {
			v.AddError("location", "must be a room number from the inventory")
		} else {
			issue.Location = strconv.Itoa(canonicalRoom)
		}
	}

	if resolved, err := app.resolveOptionalAgentField(v, "reported_by_agent", issue.ReportedByAgent, originalReportedByAgent); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	} else {
		issue.ReportedByAgent = resolved
	}
	if resolved, err := app.resolveOptionalAgentField(v, "confirmed_by_agent", issue.ConfirmedByAgent, originalConfirmedByAgent); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	} else {
		issue.ConfirmedByAgent = resolved
	}

	// Same rule as creation: if both clock times are present, they are the
	// source of truth for the stored duration.
	baseDate := issue.CreatedAt
	if baseDate.IsZero() {
		baseDate = time.Now()
	}
	if issue.StartTime != nil {
		s := strings.TrimSpace(*issue.StartTime)
		if s == "" {
			issue.StartTime = nil
			issue.StartedAt = nil
		} else {
			data.ValidateIssueTime(v, "start_time", s)
			if v.Valid() {
				t, ok := data.ParseIssueTimeWithBase(s, baseDate)
				if !ok {
					v.AddError("start_time", "must be a time, or a date and time, in 24-hour format")
				} else {
					issue.StartedAt = &t
				}
			}
		}
	}
	if issue.EndTime != nil {
		s := strings.TrimSpace(*issue.EndTime)
		if s == "" {
			issue.EndTime = nil
			issue.ResolvedAt = nil
		} else {
			data.ValidateIssueTime(v, "end_time", s)
			if v.Valid() {
				t, ok := data.ParseIssueTimeWithBase(s, baseDate)
				if !ok {
					v.AddError("end_time", "must be a time, or a date and time, in 24-hour format")
				} else {
					issue.ResolvedAt = &t
				}
			}
		}
	}
	if issue.StartedAt != nil && issue.ResolvedAt != nil {
		if !issue.ResolvedAt.After(*issue.StartedAt) {
			v.AddError("end_time", "must be after the start time")
		} else {
			issue.TimeMinutes = int(issue.ResolvedAt.Sub(*issue.StartedAt).Minutes())
		}
	}

	if input.Consumables != nil {
		if err := app.validateConsumableUses(v, *input.Consumables); err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}
	}

	if input.TVSwaps != nil {
		app.validateTVSwaps(v, *input.TVSwaps)
	}

	var supportRequests []data.SupportRequestRecord
	if input.SupportRequests != nil {
		supportRequests, err = app.validateSupportRequests(v, *input.SupportRequests)
		if err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}
	}

	if data.ValidateIssue(v, issue); !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.Issues.Update(issue); err != nil {
		switch {
		case errors.Is(err, data.ErrEditConflict):
			app.editConflictResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	if input.Consumables != nil {
		if err := app.models.Consumables.SetForIssue(issue.ID, *input.Consumables, currentUser.ID); err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}
	}

	if input.TVSwaps != nil {
		if err := app.setIssueTVSwaps(w, r, issue.ID, *input.TVSwaps, currentUser.ID); err != nil {
			return
		}
	}

	if input.SupportRequests != nil {
		if err := app.setIssueSupportRequests(w, r, issue.ID, supportRequests, currentUser.ID); err != nil {
			return
		}
	}

	// Return with user info
	fullIssue, err := app.models.Issues.Get(issue.ID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"issue": fullIssue}, nil)
}

func (app *application) deleteIssueHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	issue, err := app.models.Issues.Get(id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	// Permission: only owner or manager can delete
	currentUser := app.contextGetUser(r)
	if currentUser.Role != "manager" && issue.LoggedBy != currentUser.ID {
		app.notPermittedResponse(w, r)
		return
	}

	// The user id is passed through so the stock movements created by putting
	// this issue's consumables back are attributed to whoever deleted it.
	if err := app.models.Issues.Delete(id, currentUser.ID); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"message": "issue successfully deleted"}, nil)
}

func (app *application) statsHandler(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	date := app.readString(qs, "date", "")

	stats, err := app.models.Issues.GetStats(date)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"stats": stats}, nil)
}

func (app *application) healthcheckHandler(w http.ResponseWriter, r *http.Request) {
	env := envelope{
		"status": "available",
		"system_info": map[string]string{
			"environment": app.config.env,
			"version":     version,
		},
	}
	app.writeJSON(w, http.StatusOK, env, nil)
}
