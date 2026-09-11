package agentadmission

import (
	"context"
	"testing"
)

func TestMutationReservationIsScopedAndNonblocking(t *testing.T) {
	c := New()
	release, err := c.Reserve(context.Background(), "t", "a", "run")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reserve(context.Background(), "t", "a", "publish"); err == nil {
		t.Fatal("overlapping writer admitted")
	}
	other, err := c.Reserve(context.Background(), "other", "a", "run")
	if err != nil {
		t.Fatal(err)
	}
	other()
	release()
	release()
	next, err := c.Reserve(context.Background(), "t", "a", "publish")
	if err != nil {
		t.Fatal(err)
	}
	next()
}
