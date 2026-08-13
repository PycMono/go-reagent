package serviceimpl

import (
	"strconv"
	"testing"
)

func TestIDServiceGeneratesDistinctStringIDs(t *testing.T) {
	service, err := NewIDService(1)
	if err != nil {
		t.Fatal(err)
	}
	first := service.NextID()
	second := service.NextID()
	if first == "" || second == "" || first == second {
		t.Fatalf("generated IDs = %q, %q", first, second)
	}
	if _, err := strconv.ParseInt(first, 10, 64); err != nil {
		t.Fatalf("ID %q is not a Snowflake decimal string: %v", first, err)
	}
}

func TestIDServiceRejectsInvalidWorkerID(t *testing.T) {
	if _, err := NewIDService(1024); err == nil {
		t.Fatal("invalid Snowflake worker ID accepted")
	}
}
