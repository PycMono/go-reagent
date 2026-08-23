package serviceimpl

import (
	"strconv"
	"testing"
)

func TestIDServiceGeneratesDistinctStringIDs(t *testing.T) {
	service := NewIDService(1)
	first := service.NextID()
	second := service.NextID()
	if first == "" || second == "" || first == second {
		t.Fatalf("generated IDs = %q, %q", first, second)
	}
	if _, err := strconv.ParseInt(first, 10, 64); err != nil {
		t.Fatalf("ID %q is not a Snowflake decimal string: %v", first, err)
	}
}
