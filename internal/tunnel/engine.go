package tunnel

import (
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
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
	mu     sync.RWMutex
	clients []*ssh.Client // live pool of SSH connections
	rr     uint64         // round-robin index for picking a connection
	stopCh chan struct{}
	logger *slog.Logger
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

// currentClient returns a live ssh.Client from the pool, or nil.
func (e *Engine) currentClient() *ssh.Client {
	return e.pickClient()
}

// pickClient returns a live connection using round-robin selection so that
// tunneled traffic is spread across the pool and a single dead connection
// does not stall every client.
func (e *Engine) pickClient() *ssh.Client {
	e.mu.RLock()
	defer e.mu.RUnlock()
	n := len(e.clients)
	if n == 0 {
		return nil
	}
	start := int(atomic.AddUint64(&e.rr, 1)) % n
	for i := 0; i < n; i++ {
		if c := e.clients[(start+i)%n]; c != nil {
			return c
		}
	}
	return nil
}

func (e *Engine) addClient(c *ssh.Client) {
	e.mu.Lock()
	e.clients = append(e.clients, c)
	e.mu.Unlock()
}

func (e *Engine) removeClient(c *ssh.Client) {
	e.mu.Lock()
	out := e.clients[:0]
	for _, x := range e.clients {
		if x != c {
			out = append(out, x)
		}
	}
	e.clients = out
	e.mu.Unlock()
}

func (e *Engine) aliveCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.clients)
}

func (e *Engine) closeAll() {
	e.mu.Lock()
	for _, c := range e.clients {
		if c != nil {
			c.Close()
		}
	}
	e.clients = nil
	e.mu.Unlock()
}

// Run maintains a pool of SSH connections. In Network Resilience Mode it keeps
// up to 3 connections alive (stagger-initialized on independent timelines) and
// transparently fails over between them; in Fast Reconnect Mode it keeps a
// single connection. It returns when stopCh is closed.
func (e *Engine) Run(stopCh chan struct{}) {
	e.stopCh = stopCh
	e.statusFn(StatusDisconnected)
	e.statusFn(StatusRetrying)

	desired := 1
	kaInterval := 5 * time.Second
	kaTimeout := 10 * time.Second
	maxMissed := 2
	backoffStart := 500 * time.Millisecond
	stagger := time.Duration(0)
	if e.mode == NetworkResilience {
		desired = 3
		kaInterval = 15 * time.Second
		kaTimeout = 20 * time.Second
		maxMissed = 3
		backoffStart = 1 * time.Second
		// Offset connection lifecycles so they don't all drop in lockstep.
		stagger = 2 * time.Second
	}

	dropCh := make(chan *ssh.Client, 8)
	var wg sync.WaitGroup

	// spawnCh carries requests to dial a new pooled connection.
	spawnCh := make(chan struct{}, 8)
	requestSpawn := func() {
		select {
		case spawnCh <- struct{}{}:
		default:
		}
	}
	go func() {
		for {
			select {
			case <-stopCh:
				return
			case <-spawnCh:
				e.spawnOne(requestSpawn, dropCh, &wg, kaInterval, kaTimeout, maxMissed)
			}
		}
	}()

	// Initial pool, staggered so connections don't share a single timeline.
	for i := 0; i < desired; i++ {
		select {
		case spawnCh <- struct{}{}:
		case <-stopCh:
			e.closeAll()
			wg.Wait()
			e.statusFn(StatusDisconnected)
			return
		}
		if stagger > 0 && i < desired-1 {
			if !sleepOrStop(stagger, stopCh) {
				e.closeAll()
				wg.Wait()
				e.statusFn(StatusDisconnected)
				return
			}
		}
	}

	for {
		select {
		case <-stopCh:
			e.closeAll()
			wg.Wait()
			e.statusFn(StatusDisconnected)
			return
		case dead := <-dropCh:
			e.removeClient(dead)
			if e.aliveCount() == 0 {
				e.statusFn(StatusRetrying)
				e.logger.Warn("All connections lost. Reconnecting...")
			} else {
				e.logger.Warn("A connection dropped; failing over to the remaining pool", "remaining", e.aliveCount())
			}
			// Restore pool size after a short backoff (independent per connection).
			go func() {
				if !sleepOrStop(backoffStart, stopCh) {
					return
				}
				requestSpawn()
			}()
		}
	}
}

// spawnOne dials a single connection, registers it, and starts monitoring it.
// On failure it schedules another spawn so the pool self-heals.
func (e *Engine) spawnOne(requestSpawn func(), dropCh chan *ssh.Client, wg *sync.WaitGroup, kaInterval, kaTimeout time.Duration, maxMissed int) {
	client, err := e.dial()
	if err != nil {
		e.logger.Warn("Connect failed", "err", err)
		go func() {
			if !sleepOrStop(2*time.Second, e.stopCh) {
				return
			}
			requestSpawn()
		}()
		return
	}
	e.addClient(client)
	e.statusFn(StatusConnected)
	e.logger.Info("Connected (pool size)", "size", e.aliveCount())
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.monitor(client, dropCh, kaInterval, kaTimeout, maxMissed)
	}()
}

// monitor watches one pooled connection: a watchdog that fires the instant the
// underlying connection closes (client.Wait), plus a keepalive ticker that
// declares the connection dead only after maxMissed consecutive missed pings.
func (e *Engine) monitor(client *ssh.Client, dropCh chan *ssh.Client, interval, timeout time.Duration, maxMissed int) {
	dead := make(chan struct{}, 1)
	go func() {
		_ = client.Wait()
		select {
		case dead <- struct{}{}:
		default:
		}
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	missed := 0
	for {
		select {
		case <-e.stopCh:
			return
		case <-dead:
			client.Close()
			select {
			case dropCh <- client:
			default:
			}
			return
		case <-ticker.C:
			if sendKeepalive(client, timeout) {
				missed = 0
				continue
			}
			missed++
			e.logger.Warn("Missed keepalive", "missed", missed, "maxMissed", maxMissed)
			if missed >= maxMissed {
				e.logger.Warn("Connection considered dead via keepalive. Failing over...")
				client.Close()
				select {
				case dropCh <- client:
				default:
				}
				return
			}
		}
	}
}

// tunnelDial opens a tunneled TCP connection through any live pooled
// connection, retrying across the pool so a connection that drops at the exact
// moment of a dial is transparently handled on another pool member.
func (e *Engine) tunnelDial(target string) (net.Conn, error) {
	const maxAttempts = 5
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		c := e.pickClient()
		if c == nil {
			return nil, errTunnelDown
		}
		conn, err := c.Dial("tcp", target)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if !sleepOrStop(75*time.Millisecond, e.stopCh) {
			return nil, errTunnelDown
		}
	}
	return nil, lastErr
}

func (e *Engine) dial() (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User:            e.user,
		Auth:            []ssh.AuthMethod{ssh.Password(e.password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // acceptable for a personal single-server tool
		Timeout:         10 * time.Second,
		// Advertise a clear client version; some servers gate features on it.
		ClientVersion: "SSH-2.0-CipherProxy",
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

// Counters exposes traffic counters for the GUI to poll.
func (e *Engine) Counters() *Counters { return &e.counters }
