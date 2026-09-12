package handler

import "testing"

func TestTaskSecretLeaseRequiresPreparedDestinationAndIsSingleUse(t *testing.T) {
	s := NewTaskSecretLeaseStore()
	lease := s.Create("task-1", "runtime-1")
	if lease == "" {
		t.Fatal("Create returned an empty lease")
	}
	if err := s.Consume(lease, "task-1", "runtime-1"); err == nil {
		t.Fatal("unprepared lease was consumed")
	}
	if err := s.Prepare(lease, "task-1", "runtime-1"); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := s.Consume(lease, "task-1", "runtime-1"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if err := s.Consume(lease, "task-1", "runtime-1"); err == nil {
		t.Fatal("lease was consumed twice")
	}
}

func TestTaskSecretLeaseRejectsCrossRuntimeUse(t *testing.T) {
	s := NewTaskSecretLeaseStore()
	lease := s.Create("task-1", "runtime-1")
	if err := s.Prepare(lease, "task-1", "runtime-2"); err == nil {
		t.Fatal("lease was prepared for another runtime")
	}
}
