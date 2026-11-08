package data

import "testing"

func TestRoomFloor(t *testing.T) {
	tests := []struct {
		room int
		want int
	}{
		{1214, 1},
		{1234, 1},
		{4120, 4},
		{4512, 4},
		{7, 7},
		{0, 0},
		{-5, 0},
	}
	for _, tc := range tests {
		if got := RoomFloor(tc.room); got != tc.want {
			t.Errorf("RoomFloor(%d) = %d, want %d", tc.room, got, tc.want)
		}
	}
}

func TestComputeRoomStatus(t *testing.T) {
	tv := CategoryEquipment{EquipmentTypeID: 1, Name: "TV", Quantity: 1}
	phone := CategoryEquipment{EquipmentTypeID: 2, Name: "Phone", Quantity: 1}
	twoTVs := CategoryEquipment{EquipmentTypeID: 1, Name: "TV", Quantity: 2}

	tests := []struct {
		name      string
		category  *RoomCategory
		equipment []RoomEquipmentCount
		surveyed  bool
		want      string
	}{
		{
			name:      "no category is unassigned even if equipment was recorded",
			category:  nil,
			equipment: []RoomEquipmentCount{{EquipmentTypeID: 1, Present: 1, Working: 1}},
			surveyed:  true,
			want:      RoomStatusUnassigned,
		},
		{
			name:     "category but no survey is unchecked",
			category: &RoomCategory{Equipment: []CategoryEquipment{tv}},
			surveyed: false,
			want:     RoomStatusUnchecked,
		},
		{
			name:     "category expecting nothing is ok",
			category: &RoomCategory{Equipment: []CategoryEquipment{}},
			surveyed: true,
			want:     RoomStatusOK,
		},
		{
			name:     "every expected item present and working",
			category: &RoomCategory{Equipment: []CategoryEquipment{tv, phone}},
			equipment: []RoomEquipmentCount{
				{EquipmentTypeID: 1, Present: 1, Working: 1},
				{EquipmentTypeID: 2, Present: 1, Working: 1},
			},
			surveyed: true,
			want:     RoomStatusOK,
		},
		{
			name:     "one set of two is broken",
			category: &RoomCategory{Equipment: []CategoryEquipment{twoTVs}},
			equipment: []RoomEquipmentCount{
				{EquipmentTypeID: 1, Present: 2, Working: 1},
			},
			surveyed: true,
			want:     RoomStatusDegraded,
		},
		{
			name:     "nothing expected is working",
			category: &RoomCategory{Equipment: []CategoryEquipment{twoTVs}},
			equipment: []RoomEquipmentCount{
				{EquipmentTypeID: 1, Present: 2, Working: 0},
			},
			surveyed: true,
			want:     RoomStatusDown,
		},
		{
			name:     "one working item out of two types is degraded",
			category: &RoomCategory{Equipment: []CategoryEquipment{tv, phone}},
			equipment: []RoomEquipmentCount{
				{EquipmentTypeID: 1, Present: 1, Working: 1},
				{EquipmentTypeID: 2, Present: 1, Working: 0},
			},
			surveyed: true,
			want:     RoomStatusDegraded,
		},
		{
			name:     "an extra working router does not rescue a dead TV",
			category: &RoomCategory{Equipment: []CategoryEquipment{tv}},
			equipment: []RoomEquipmentCount{
				{EquipmentTypeID: 1, Present: 1, Working: 0},
				{EquipmentTypeID: 3, Present: 1, Working: 1},
			},
			surveyed: true,
			want:     RoomStatusDown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ComputeRoomStatus(tc.category, tc.equipment, tc.surveyed); got != tc.want {
				t.Errorf("ComputeRoomStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAddExpectedIncludesUncheckedEquipment(t *testing.T) {
	category := &RoomCategory{Equipment: []CategoryEquipment{
		{EquipmentTypeID: 1, Name: "TV", Quantity: 2},
	}}
	got := addExpected(nil, category)
	if len(got) != 1 {
		t.Fatalf("addExpected() returned %d rows, want 1", len(got))
	}
	if got[0].Expected != 2 || got[0].Present != 0 || got[0].Working != 0 {
		t.Errorf("addExpected() = %+v, want expected 2 with zero counts", got[0])
	}
}
