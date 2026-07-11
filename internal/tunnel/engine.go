package tunnel

import (
	"log/slog"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type Mode int

const (
	FastReconnect Mode = iota
	NetworkResilience
)

type Status int

const (
	StatusDisconnected Status = iota
	StatusConnected
	StatusRetrying
)

type StatusFunc func(Status)

type Engine struct {
	mu       sync.RWMutex
	client   *ssh.Client
	stopCh   chan struct{}
	logger   *slog.Logger
	statusFn StatusFunc
	counters Counters

	addr, user, password string
	mode                 Mode
}

func NewEngine(addr, user, password string, mode Mode, logger *slog.Logger, statusFn StatusFunc) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		addr: addr, user: user, password: password, mode: mode,
		logger: logger, statusFn: statusFn,
	}
}

// currentClient returns the live ssh.Client, or nil if currently disconnected/reconnecting.
func (e *Engine) currentClient() *ssh.Client {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.client
}

func (e *Engine) setClient(c *ssh.Client) {
	e.mu.Lock()
	e.client = c
	e.mu.Unlock()
}

// Run manages the connect/keepalive/reconnect loop until Stop() is called.
func (e *Engine) Run(stopCh chan struct{}) {
	e.stopCh = stopCh
	e.statusFn(StatusDisconnected)

	keepaliveInterval := 5 * time.Second
	keepaliveTimeout := 3 * time.Second
	maxMissed := 1
	backoffStart := 500 * time.Millisecond
	backoffCap := 5 * time.Second
	if e.mode == NetworkResilience {
		keepaliveInterval = 15 * time.Second
		keepaliveTimeout = 8 * time.Second
		maxMissed = 3
		backoffStart = 1 * time.Second
		backoffCap = 20 * time.Second
	}

	backoff := backoffStart

	for {
		select {
		case <-stopCh:
			e.setClient(nil)
			e.statusFn(StatusDisconnected)
			return
		default:
		}

		e.logger.Info("Connecting to server", "user", e.user, "addr", e.addr)
		client, err := e.dial()
		if err != nil {
			e.statusFn(StatusRetrying)
			e.logger.Warn("Connect failed", "err", err, "retryIn", backoff.String())
			e.setClient(nil)
			if !sleepOrStop(backoff, stopCh) {
				e.statusFn(StatusDisconnected)
				return
			}
			backoff = nextBackoff(backoff, backoffCap)
			continue
		}

		e.logger.Info("Connected.")
		backoff = backoffStart
		e.setClient(client)
		e.statusFn(StatusConnected)

		// Keepalive loop for this connection.
		missed := 0
		died := false
		for !died {
			select {
			case <-stopCh:
				client.Close()
				e.setClient(nil)
				e.statusFn(StatusDisconnected)
				return
			case <-time.After(keepaliveInterval):
				ok := sendKeepalive(client, keepaliveTimeout)
				if !ok {
					missed++
					e.logger.Warn("Missed keepalive", "missed", missed, "maxMissed", maxMissed)
					if missed >= maxMissed {
						e.logger.Warn("Connection considered dead. Reconnecting...")
						client.Close()
						e.setClient(nil)
						e.statusFn(StatusRetrying)
						died = true
					}
				} else {
					missed = 0
				}
			}
		}
	}
}

func (e *Engine) dial() (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User:            e.user,
		Auth:            []ssh.AuthMethod{ssh.Password(e.password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // acceptable for a personal single-server tool
		Timeout:         8 * time.Second,
	}
	return ssh.Dial("tcp", e.addr, cfg)
}

func sendKeepalive(client *ssh.Client, timeout time.Duration) bool {
	type result struct {
		ok bool
	}
	done := make(chan result, 1)
	go func() {
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		done <- result{ok: err == nil}
	}()
	select {
	case r := <-done:
		return r.ok
	case <-time.After(timeout):
		return false
	}
}

func sleepOrStop(d time.Duration, stopCh chan struct{}) bool {
	select {
	case <-time.After(d):
		return true
	case <-stopCh:
		return false
	}
}

func nextBackoff(cur, cap time.Duration) time.Duration {
	next := cur * 2
	if next > cap {
		return cap
	}
	return next
}

// Counters exposes traffic counters for the GUI to poll.
func (e *Engine) Counters() *Counters { return &e.counters }
