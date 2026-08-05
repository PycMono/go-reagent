package mysql

import (
	"reflect"
	"testing"
)

func TestSafeWindow(t *testing.T) {
	tests := []struct {
		name  string
		rows  []messageRow
		limit int
		want  []uint64
	}{
		{
			name:  "extra row belongs to older turn",
			rows:  windowRows([2]uint64{4, 2}, [2]uint64{3, 2}, [2]uint64{2, 1}),
			limit: 4,
			want:  []uint64{2, 3, 4, 5},
		},
		{
			name:  "extra row shares oldest selected turn",
			rows:  windowRows([2]uint64{3, 2}, [2]uint64{2, 2}),
			limit: 3,
			want:  []uint64{3, 4},
		},
		{
			name:  "single turn exceeds limit",
			rows:  windowRows([2]uint64{1, 3}),
			limit: 2,
			want:  nil,
		},
		{
			name:  "at most limit rows",
			rows:  windowRows([2]uint64{3, 1}, [2]uint64{2, 1}, [2]uint64{1, 1}),
			limit: 3,
			want:  []uint64{1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRows := safeWindow(tt.rows, tt.limit)
			var got []uint64
			for index := range gotRows {
				got = append(got, gotRows[index].ID)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("safeWindow() IDs = %v, want %v", got, tt.want)
			}
		})
	}
}

func windowRows(turnCounts ...[2]uint64) []messageRow {
	var rows []messageRow
	var id uint64
	for index := len(turnCounts) - 1; index >= 0; index-- {
		id += turnCounts[index][1]
	}
	for _, pair := range turnCounts {
		turnVersion, count := pair[0], pair[1]
		for ordinal := count; ordinal > 0; ordinal-- {
			rows = append(rows, messageRow{ID: id, TurnVersion: turnVersion, Ordinal: uint32(ordinal - 1)})
			id--
		}
	}
	return rows
}
