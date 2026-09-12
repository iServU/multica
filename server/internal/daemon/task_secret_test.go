package daemon

import (
	"context"
	"testing"
	"time"
)

func newTaskSecretTestDaemon() *Daemon {
	return &Daemon{
		taskSecrets:       make(map[string]taskSecret),
		taskSecretWaiters: make(map[string]chan taskSecret),
	}
}

func TestTaskSecretLeaseIsBoundToTaskAndConsumedOnce(t *testing.T) {
	d := newTaskSecretTestDaemon()
	if d.acceptTaskSecret(taskSecret{taskID: "task-a", leaseID: "lease-a", envKey: "FORGEJO_TOKEN", secret: "opaque"}) != true {
		t.Fatal("first lease delivery was rejected")
	}
	if _, err := d.takeTaskSecret(context.Background(), "task-b", "lease-a"); err == nil {
		t.Fatal("lease for another task was accepted")
	}
	if d.acceptTaskSecret(taskSecret{taskID: "task-a", leaseID: "lease-a", envKey: "FORGEJO_TOKEN", secret: "opaque-2"}) {
		t.Fatal("duplicate lease was accepted")
	}
	got, err := d.takeTaskSecret(context.Background(), "task-a", "lease-a")
	if err != nil || got.secret != "opaque" {
		t.Fatalf("takeTaskSecret() = %#v, %v", got, err)
	}
	if _, err := d.takeTaskSecret(context.Background(), "task-a", "lease-a"); err == nil {
		t.Fatal("lease was reusable")
	}
}

func TestTaskSecretLeaseRejectsUnsafeEnvironmentAndTimesOut(t *testing.T) {
	d := newTaskSecretTestDaemon()
	if d.acceptTaskSecret(taskSecret{taskID: "task-a", leaseID: "lease-a", envKey: "MULTICA_TOKEN", secret: "opaque"}) {
		t.Fatal("protected environment key was accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := d.takeTaskSecret(ctx, "task-a", "missing"); err == nil {
		t.Fatal("missing lease did not time out")
	}
	d.clearTaskSecret("missing")
	if len(d.taskSecrets) != 0 || len(d.taskSecretWaiters) != 0 {
		t.Fatal("timed out lease was retained")
	}
}
