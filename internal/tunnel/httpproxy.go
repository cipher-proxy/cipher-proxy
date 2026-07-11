package tunnel

import (
	"bufio"
	"io"
	"net"
	"net/http"

	"log/slog"
)

func StartHTTPProxyServer(port int, engine *Engine, router *Router, logger *slog.Logger) (net.Listener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", itoa(port)))
	if err != nil {
		return nil, err
	}
	server := &http.Server{
		Handler: httpProxyHandler(engine, router, logger),
	}
	go server.Serve(ln)
	return ln, nil
}

func httpProxyHandler(engine *Engine, router *Router, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		excluded := router != nil && router.Match(r.Host)
		if !excluded && engine.currentClient() == nil {
			logger.Warn("HTTP request rejected: upstream SSH tunnel not connected", "method", r.Method, "host", r.Host)
			http.Error(w, "upstream SSH tunnel not connected", http.StatusBadGateway)
			return
		}

		if r.Method == http.MethodConnect {
			handleConnect(w, r, engine, router, logger)
			return
		}
		handleForward(w, r, engine, router, logger)
	}
}

func handleConnect(w http.ResponseWriter, r *http.Request, engine *Engine, router *Router, logger *slog.Logger) {
	excluded := router != nil && router.Match(r.Host)
	remote, err := dialForTarget(r.Host, engine, router)
	if err != nil {
		if excluded {
			logger.Warn("CONNECT direct failed (blacklisted)", "host", r.Host, "err", err)
		} else {
			logger.Warn("CONNECT via SSH failed", "host", r.Host, "err", err)
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer remote.Close()

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	defer clientConn.Close()
	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	wrappedRemote := wrapConn(remote, engine.Counters())
	pipe(clientConn, wrappedRemote)
}

func handleForward(w http.ResponseWriter, r *http.Request, engine *Engine, router *Router, logger *slog.Logger) {
	host := r.Host
	if r.URL.Port() == "" {
		host = host + ":80"
	}
	remote, err := dialForTarget(host, engine, router)
	if err != nil {
		logger.Warn("Forward dial failed", "host", host, "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer remote.Close()

	wrappedRemote := wrapConn(remote, engine.Counters())
	if err := r.Write(wrappedRemote); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	resp, err := http.ReadResponse(newBufReader(wrappedRemote), r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func newBufReader(c net.Conn) *bufio.Reader {
	return bufio.NewReader(c)
}
