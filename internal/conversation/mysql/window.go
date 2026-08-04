package mysql

import "slices"

func safeWindow(rows []messageRow, limit int) []messageRow {
	if limit < 1 || len(rows) == 0 {
		return nil
	}
	selected := append([]messageRow(nil), rows...)
	if len(selected) > limit {
		extra := selected[limit]
		selected = selected[:limit]
		oldestTurn := selected[len(selected)-1].TurnVersion
		if extra.TurnVersion == oldestTurn {
			for len(selected) > 0 && selected[len(selected)-1].TurnVersion == oldestTurn {
				selected = selected[:len(selected)-1]
			}
		}
	}
	slices.Reverse(selected)
	return selected
}
