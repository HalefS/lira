package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// The room map's read side is one response: the categories, the equipment
// catalog and every room with its derived status. A hotel has a few hundred
// rooms at most, and splitting that into several round trips would make the page
// slower without making either half simpler.
func (app *application) listRoomMapHandler(w http.ResponseWriter, r *http.Request) {
	snapshot, err := app.models.RoomMap.RoomMap()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"room_map": snapshot}, nil)
}

func (app *application) showRoomMapRoomHandler(w http.ResponseWriter, r *http.Request) {
	number, err := app.readIDParamNamed(r, "room")
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	room, err := app.models.RoomMap.Room(int(number))
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"room": room}, nil)
}

// saveRoomMapRoomHandler records a category and a survey for one room.
//
// Any authenticated user may write it, like a maintenance check: the people who
// walk the rooms and count the televisions are the technicians, not the managers
// who define the categories.
func (app *application) saveRoomMapRoomHandler(w http.ResponseWriter, r *http.Request) {
	number, err := app.readIDParamNamed(r, "room")
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	var input struct {
		CategoryID *int64 `json:"category_id"`
		Notes      string `json:"notes"`
		Surveyed   bool   `json:"surveyed"`
		Equipment  []struct {
			EquipmentTypeID int64 `json:"equipment_type_id"`
			Present         int   `json:"present"`
			Working         int   `json:"working"`
		} `json:"equipment"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	equipment := make([]data.RoomEquipmentInput, 0, len(input.Equipment))
	for _, e := range input.Equipment {
		equipment = append(equipment, data.RoomEquipmentInput{
			EquipmentTypeID: e.EquipmentTypeID,
			Present:         e.Present,
			Working:         e.Working,
		})
	}

	v := validator.New()
	if input.CategoryID != nil && *input.CategoryID < 1 {
		v.AddError("category_id", "must be a valid category")
	}
	data.ValidateRoomEquipment(v, equipment)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	user := app.contextGetUser(r)
	err = app.models.RoomMap.SaveRoom(int(number), data.SaveRoomInput{
		CategoryID: input.CategoryID,
		Notes:      input.Notes,
		Surveyed:   input.Surveyed,
		Equipment:  equipment,
	}, user.ID)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRoomNotInInventory):
			v.AddError("room", "this room is not in the inventory")
			app.failedValidationResponse(w, r, v.Errors)
		case errors.Is(err, data.ErrUnknownEquipmentType):
			v.AddError("equipment", "one of the selected equipment types no longer exists")
			app.failedValidationResponse(w, r, v.Errors)
		case errors.Is(err, data.ErrRecordNotFound):
			v.AddError("category_id", "this category no longer exists")
			app.failedValidationResponse(w, r, v.Errors)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	room, err := app.models.RoomMap.Room(int(number))
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"room": room}, nil)
}

type roomCategoryEquipmentInput struct {
	EquipmentTypeID int64 `json:"equipment_type_id"`
	Quantity        int   `json:"quantity"`
}

type roomCategoryInput struct {
	Name      *string                       `json:"name"`
	Color     *string                       `json:"color"`
	Equipment *[]roomCategoryEquipmentInput `json:"equipment"`
}

func categoryEquipmentFromInput(in []roomCategoryEquipmentInput) []data.CategoryEquipment {
	out := make([]data.CategoryEquipment, 0, len(in))
	for _, e := range in {
		out = append(out, data.CategoryEquipment{
			EquipmentTypeID: e.EquipmentTypeID,
			Quantity:        e.Quantity,
		})
	}
	return out
}

func (app *application) listRoomCategoriesHandler(w http.ResponseWriter, r *http.Request) {
	categories, err := app.models.RoomMap.Categories()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"room_categories": categories}, nil)
}

func (app *application) createRoomCategoryHandler(w http.ResponseWriter, r *http.Request) {
	var input roomCategoryInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	name := ""
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
	}
	color := ""
	if input.Color != nil {
		color = *input.Color
	}
	equipment := []data.CategoryEquipment{}
	if input.Equipment != nil {
		equipment = categoryEquipmentFromInput(*input.Equipment)
	}

	v := validator.New()
	data.ValidateRoomCategory(v, name, color)
	data.ValidateCategoryEquipment(v, equipment)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	user := app.contextGetUser(r)
	category, err := app.models.RoomMap.CreateCategory(name, color, user.ID, equipment)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrDuplicateRoomCategory):
			v.AddError("name", "a category with this name already exists")
			app.failedValidationResponse(w, r, v.Errors)
		case errors.Is(err, data.ErrUnknownEquipmentType):
			v.AddError("equipment", "one of the selected equipment types no longer exists")
			app.failedValidationResponse(w, r, v.Errors)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusCreated, envelope{"room_category": category}, nil)
}

// updateRoomCategoryHandler changes a category's name, colour or template.
//
// Fields the caller omits are left alone. The UI sends all three because its
// editor always has them, but a partial PATCH should not silently rename a
// category to "" or clear its template just because the field was absent.
func (app *application) updateRoomCategoryHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	var input roomCategoryInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	existing, err := app.models.RoomMap.Category(id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	name := existing.Name
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
	}
	color := existing.Color
	if input.Color != nil {
		color = *input.Color
	}
	equipment := existing.Equipment
	if input.Equipment != nil {
		equipment = categoryEquipmentFromInput(*input.Equipment)
	}

	v := validator.New()
	data.ValidateRoomCategory(v, name, color)
	data.ValidateCategoryEquipment(v, equipment)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	category, err := app.models.RoomMap.UpdateCategory(id, name, color, equipment)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		case errors.Is(err, data.ErrDuplicateRoomCategory):
			v.AddError("name", "a category with this name already exists")
			app.failedValidationResponse(w, r, v.Errors)
		case errors.Is(err, data.ErrUnknownEquipmentType):
			v.AddError("equipment", "one of the selected equipment types no longer exists")
			app.failedValidationResponse(w, r, v.Errors)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"room_category": category}, nil)
}

func (app *application) deleteRoomCategoryHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	if err := app.models.RoomMap.DeleteCategory(id); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"message": "room category successfully deleted"}, nil)
}

func (app *application) listEquipmentTypesHandler(w http.ResponseWriter, r *http.Request) {
	types, err := app.models.RoomMap.EquipmentTypes()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"equipment_types": types}, nil)
}

func (app *application) createEquipmentTypeHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
		Icon string `json:"icon"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	v := validator.New()
	data.ValidateEquipmentType(v, input.Name, input.Icon)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	user := app.contextGetUser(r)
	equipmentType, err := app.models.RoomMap.CreateEquipmentType(input.Name, input.Icon, user.ID)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrDuplicateEquipmentType):
			v.AddError("name", "an equipment type with this name already exists")
			app.failedValidationResponse(w, r, v.Errors)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusCreated, envelope{"equipment_type": equipmentType}, nil)
}

func (app *application) deleteEquipmentTypeHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	if err := app.models.RoomMap.DeleteEquipmentType(id); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"message": "equipment type successfully deleted"}, nil)
}
