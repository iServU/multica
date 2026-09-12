package daemon

import (
	"context"
	"errors"
	"strings"
	"time"
)

var errTaskSecretUnavailable = errors.New("task secret lease unavailable")

const taskSecretTombstoneTTL = 10 * time.Minute

type taskSecret struct {
	taskID  string
	leaseID string
	envKey  string
	secret  string
}

func validTaskSecretEnvKey(key string) bool {
	// Keep this path intentionally narrow: it is for brokered credentials, not
	// an arbitrary replacement for the daemon's protected environment.
	return key == "FORGEJO_TOKEN"
}

func (d *Daemon) pruneTaskSecretTombstones(now time.Time) {
	for leaseID, closedAt := range d.taskSecretClosed {
		if now.Sub(closedAt) >= taskSecretTombstoneTTL {
			delete(d.taskSecretClosed, leaseID)
		}
	}
}

func (d *Daemon) closeTaskSecretLease(leaseID string) {
	if d.taskSecretClosed == nil {
		d.taskSecretClosed = make(map[string]time.Time)
	}
	d.taskSecretClosed[leaseID] = time.Now()
}

func (d *Daemon) acceptTaskSecret(secret taskSecret) bool {
	if secret.taskID == "" || secret.leaseID == "" || secret.secret == "" || !validTaskSecretEnvKey(secret.envKey) {
		return false
	}
	d.taskSecretsMu.Lock()
	defer d.taskSecretsMu.Unlock()
	if d.taskSecrets == nil {
		d.taskSecrets = make(map[string]taskSecret)
	}
	if d.taskSecretWaiters == nil {
		d.taskSecretWaiters = make(map[string]chan taskSecret)
	}
	d.pruneTaskSecretTombstones(time.Now())
	if _, closed := d.taskSecretClosed[secret.leaseID]; closed {
		return false
	}
	if waiter, ok := d.taskSecretWaiters[secret.leaseID]; ok {
		delete(d.taskSecretWaiters, secret.leaseID)
		d.closeTaskSecretLease(secret.leaseID)
		waiter <- secret
		close(waiter)
		return true
	}
	// A lease may arrive just before the task reaches the final launch seam.
	// Keep one value in memory until that seam, never in a task or file.
	if _, exists := d.taskSecrets[secret.leaseID]; exists {
		return false
	}
	d.taskSecrets[secret.leaseID] = secret
	return true
}

func (d *Daemon) takeTaskSecret(ctx context.Context, taskID, leaseID string) (taskSecret, error) {
	if leaseID == "" {
		return taskSecret{}, nil
	}
	d.taskSecretsMu.Lock()
	if d.taskSecretWaiters == nil {
		d.taskSecretWaiters = make(map[string]chan taskSecret)
	}
	d.pruneTaskSecretTombstones(time.Now())
	if _, closed := d.taskSecretClosed[leaseID]; closed {
		d.taskSecretsMu.Unlock()
		return taskSecret{}, errTaskSecretUnavailable
	}
	if secret, ok := d.taskSecrets[leaseID]; ok {
		if secret.taskID != taskID {
			d.taskSecretsMu.Unlock()
			return taskSecret{}, errTaskSecretUnavailable
		}
		delete(d.taskSecrets, leaseID)
		d.closeTaskSecretLease(leaseID)
		d.taskSecretsMu.Unlock()
		return secret, nil
	}
	if _, waiting := d.taskSecretWaiters[leaseID]; waiting {
		d.taskSecretsMu.Unlock()
		return taskSecret{}, errTaskSecretUnavailable
	}
	waiter := make(chan taskSecret, 1)
	d.taskSecretWaiters[leaseID] = waiter
	d.taskSecretsMu.Unlock()
	defer func() {
		d.taskSecretsMu.Lock()
		delete(d.taskSecretWaiters, leaseID)
		d.taskSecretsMu.Unlock()
	}()
	select {
	case secret := <-waiter:
		if secret.leaseID != leaseID || secret.taskID != taskID {
			return taskSecret{}, errTaskSecretUnavailable
		}
		return secret, nil
	case <-ctx.Done():
		return taskSecret{}, errors.Join(errTaskSecretUnavailable, ctx.Err())
	}
}

func (d *Daemon) clearTaskSecret(leaseID string) {
	d.taskSecretsMu.Lock()
	d.pruneTaskSecretTombstones(time.Now())
	delete(d.taskSecrets, leaseID)
	d.closeTaskSecretLease(leaseID)
	if waiter, ok := d.taskSecretWaiters[leaseID]; ok {
		delete(d.taskSecretWaiters, leaseID)
		close(waiter)
	}
	d.taskSecretsMu.Unlock()
}

func (s taskSecret) validFor(taskID string) bool {
	return strings.TrimSpace(taskID) != "" && s.taskID == taskID && s.leaseID != "" && validTaskSecretEnvKey(s.envKey) && s.secret != ""
}
