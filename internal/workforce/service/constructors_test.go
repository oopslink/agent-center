package service

import (
	"context"
	"testing"
)

// Tests that constructors accept nil clock (defaulting to SystemClock).
func TestNewWorkerEnrollService_NilClock(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, nil)
	res, err := enroll.Enroll(context.Background(), EnrollCommand{
		WorkerID:      "W-1",
		ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.WorkerID != "W-1" {
		t.Fatal()
	}
}
