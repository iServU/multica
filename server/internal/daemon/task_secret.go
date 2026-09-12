package daemon

import (
	"context"
	"errors"
	"strings"
)

var errTaskSecretUnavailable = errors.New("task secret lease unavailable")

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
	if waiter, ok := d.taskSecretWaiters[secret.leaseID]; ok {
		delete(d.taskSecretWaiters, secret.leaseID)
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
	if secret, ok := d.taskSecrets[leaseID]; ok {
		if secret.taskID != taskID {
			d.taskSecretsMu.Unlock()
			return taskSecret{}, errTaskSecretUnavailable
		}
		delete(d.taskSecrets, leaseID)
		d.taskSecretsMu.Unlock()
		return secret, nil
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
	delete(d.taskSecrets, leaseID)
	if waiter, ok := d.taskSecretWaiters[leaseID]; ok {
		delete(d.taskSecretWaiters, leaseID)
		close(waiter)
	}
	d.taskSecretsMu.Unlock()
}

func (s taskSecret) validFor(taskID string) bool {
	return strings.TrimSpace(taskID) != "" && s.taskID == taskID && s.leaseID != "" && validTaskSecretEnvKey(s.envKey) && s.secret != ""
}
