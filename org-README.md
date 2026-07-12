# cipher-proxy

**Open, privacy-respecting tunneling tools — built on SSH.**

`cipher-proxy` is a small, community-driven organization maintaining lightweight
tools that turn a single standard SSH server into a fast, predictable SOCKS5/HTTP
proxy. No extra server software, no accounts, no telemetry — just encrypted
tunnels you fully control.

## Why

Public Wi-Fi, restrictive networks, and unreliable links make naive proxies
fragile. Everything here is **based on SSH**: your traffic rides inside a normal
SSH session to a server you already own, so it works anywhere `ssh` works.

## Projects

- **[cipher-proxy](https://github.com/cipher-proxy/cipher-proxy)** — the main app:
  a Go + Fyne GUI that keeps local proxy listeners alive while transparently
  reconnecting the underlying SSH tunnel (Fast Reconnect / Network Resilience
  modes), with active keepalive detection for unstable links.

## Join & contribute

We welcome contributors of all levels:

- **Code** — Go, Fyne/GUI, packaging, docs.
- **Testing** — especially on macOS, Windows, and ARM devices.
- **Ideas** — new tunnel modes, better reconnect heuristics, platform installers.

To get started:

1. Fork a repository and open a pull request.
2. Read the project README and follow its build/test conventions.
3. Keep logs via `slog`, and run `go vet ./...` before submitting.

Questions or want to help maintain something? Open an issue or start a
discussion in any of the org repositories.
