package tunnel

import (
	"net"
	"sync/atomic"
)

// Counters holds cumulative byte counts, safe for concurrent use.
type Counters struct {
	BytesIn  int64
	BytesOut int64
}

func (c *Counters) AddIn(n int64)  { atomic.AddInt64(&c.BytesIn, n) }
func (c *Counters) AddOut(n int64) { atomic.AddInt64(&c.BytesOut, n) }
func (c *Counters) SnapshotIn() int64  { return atomic.LoadInt64(&c.BytesIn) }
func (c *Counters) SnapshotOut() int64 { return atomic.LoadInt64(&c.BytesOut) }

// countingConn wraps a net.Conn and reports bytes through Counters.
// "In" = bytes read from remote into local client (download).
// "Out" = bytes written from local client to remote (upload).
type countingConn struct {
	net.Conn
	counters *Counters
}

func (c *countingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.counters.AddIn(int64(n))
	}
	return n, err
}

func (c *countingConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 {
		c.counters.AddOut(int64(n))
	}
	return n, err
}

func wrapConn(conn net.Conn, counters *Counters) net.Conn {
	return &countingConn{Conn: conn, counters: counters}
}
