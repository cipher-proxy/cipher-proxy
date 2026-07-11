package tunnel

import (
	"encoding/binary"
	"io"
	"net"

	"log/slog"
)

func StartSocksServer(port int, engine *Engine, router *Router, logger *slog.Logger) (net.Listener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", itoa(port)))
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go handleSocksConn(conn, engine, router, logger)
		}
	}()
	return ln, nil
}

func handleSocksConn(conn net.Conn, engine *Engine, router *Router, logger *slog.Logger) {
	defer conn.Close()

	// Greeting: VER NMETHODS METHODS...
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return
	}
	nmethods := int(buf[1])
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	conn.Write([]byte{0x05, 0x00}) // no auth required

	// Request: VER CMD RSV ATYP DST.ADDR DST.PORT
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return
	}
	if head[1] != 0x01 { // only CONNECT supported
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var target string
	switch head[3] {
	case 0x01: // IPv4
		ip := make([]byte, 4)
		io.ReadFull(conn, ip)
		portB := make([]byte, 2)
		io.ReadFull(conn, portB)
		target = net.JoinHostPort(net.IP(ip).String(), itoa(int(binary.BigEndian.Uint16(portB))))
	case 0x03: // domain name
		l := make([]byte, 1)
		io.ReadFull(conn, l)
		name := make([]byte, l[0])
		io.ReadFull(conn, name)
		portB := make([]byte, 2)
		io.ReadFull(conn, portB)
		target = net.JoinHostPort(string(name), itoa(int(binary.BigEndian.Uint16(portB))))
	case 0x04: // IPv6
		ip := make([]byte, 16)
		io.ReadFull(conn, ip)
		portB := make([]byte, 2)
		io.ReadFull(conn, portB)
		target = net.JoinHostPort(net.IP(ip).String(), itoa(int(binary.BigEndian.Uint16(portB))))
	default:
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	excluded := router != nil && router.Match(target)
	remote, err := dialForTarget(target, engine, router)
	if err != nil {
		if excluded {
			logger.Warn("SOCKS direct dial failed (blacklisted)", "target", target, "err", err)
		} else {
			logger.Warn("SOCKS dial via SSH failed", "target", target, "err", err)
		}
		conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // general failure — fail fast
		return
	}
	defer remote.Close()

	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // success

	counters := engine.Counters()
	wrappedRemote := wrapConn(remote, counters)
	pipe(conn, wrappedRemote)
}

func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { io.Copy(a, b); done <- struct{}{} }()
	go func() { io.Copy(b, a); done <- struct{}{} }()
	<-done
}
