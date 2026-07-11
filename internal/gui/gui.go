package gui

import (
	"fmt"
	"image/color"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"cipherproxy/internal/config"
	"cipherproxy/internal/log"
	"cipherproxy/internal/tunnel"
)

var (
	colorRed    = color.RGBA{R: 0xff, A: 0xff}
	colorGreen  = color.RGBA{G: 0xff, A: 0xff}
	colorOrange = color.RGBA{R: 0xff, G: 0xa5, B: 0x00, A: 0xff}
	// colorStart is the Start button color (#065f46).
	colorStart = color.RGBA{R: 0x06, G: 0x5f, B: 0x46, A: 0xff}
	// colorAddrText is a light, readable color for the (disabled) proxy
	// address boxes so the "http://..." / "socks5://..." text is easy to read.
	colorAddrText = color.RGBA{R: 0xe6, G: 0xe6, B: 0xe6, A: 0xff}
)

// greenTheme makes the primary/accent color (used by HighImportance buttons
// such as Start) match the requested Start-button green (#065f46), and lightens
// the disabled-text color so the read-only proxy address boxes are legible.
type greenTheme struct {
	fyne.Theme
}

func (t greenTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNamePrimary:
		return colorStart
	case theme.ColorNameDisabled:
		return colorAddrText
	}
	return t.Theme.Color(n, v)
}

// actionButton is a Button with an explicit minimum size so the Start/Stop
// button can be larger than the Ping button.
type actionButton struct {
	widget.Button
}

func newActionButton(text string, onTapped func()) *actionButton {
	b := &actionButton{}
	b.Text = text
	b.OnTapped = onTapped
	b.ExtendBaseWidget(b)
	return b
}

func (b *actionButton) MinSize() fyne.Size {
	return fyne.NewSize(72, 30)
}

func defaultPort(v, d int) string {
	if v == 0 {
		return strconv.Itoa(d)
	}
	return strconv.Itoa(v)
}

// measurePing performs a TCP connect-timing "ping" to addr and returns the
// round-trip time in milliseconds.
func measurePing(addr string) (int64, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return time.Since(start).Milliseconds(), nil
}

func Run() {
	a := app.NewWithID("com.cipherproxy.app")
	a.SetIcon(AppIcon())
	a.Settings().SetTheme(greenTheme{theme.DarkTheme()})
	w := a.NewWindow("Cipher Proxy")
	w.SetIcon(AppIcon())

	cfg, err := config.Load()
	if err != nil {
		slog.Warn("could not load saved config, using defaults", "err", err)
		cfg = &config.Settings{}
	}
	if cfg == nil {
		cfg = &config.Settings{}
	}

	addrEntry := widget.NewEntry()
	addrEntry.SetText(cfg.Address)
	portEntry := widget.NewEntry()
	portEntry.SetText(defaultPort(cfg.Port, 22))
	userEntry := widget.NewEntry()
	userEntry.SetText(cfg.User)
	passEntry := widget.NewPasswordEntry()
	passEntry.SetText(cfg.Password)

	httpPortEntry := widget.NewEntry()
	httpPortEntry.SetText(defaultPort(cfg.HTTPProxyPort, 18888))
	socksPortEntry := widget.NewEntry()
	socksPortEntry.SetText(defaultPort(cfg.SocksProxyPort, 11080))

	// Immutable proxy-address display boxes (with Copy buttons), full width.
	httpAddrEntry := widget.NewEntry()
	httpAddrEntry.Disable()
	socksAddrEntry := widget.NewEntry()
	socksAddrEntry.Disable()
	copyHTTP := widget.NewButton("Copy", func() { w.Clipboard().SetContent(httpAddrEntry.Text) })
	copySocks := widget.NewButton("Copy", func() { w.Clipboard().SetContent(socksAddrEntry.Text) })
	updateAddr := func() {
		h := strings.TrimSpace(httpPortEntry.Text)
		if h == "" {
			h = "18888"
		}
		s := strings.TrimSpace(socksPortEntry.Text)
		if s == "" {
			s = "11080"
		}
		httpAddrEntry.SetText("http://127.0.0.1:" + h)
		socksAddrEntry.SetText("socks5://127.0.0.1:" + s)
	}
	httpPortEntry.OnChanged = func(string) { updateAddr() }
	socksPortEntry.OnChanged = func(string) { updateAddr() }
	updateAddr()

	resilienceCheck := widget.NewCheck("Network Resilience Mode", nil)
	resilienceCheck.Checked = cfg.ResilienceMode
	fastCheck := widget.NewCheck("Fast Reconnect Mode", nil)
	fastCheck.Checked = cfg.FastReconnect
	resilienceCheck.OnChanged = func(v bool) {
		if v {
			fastCheck.SetChecked(false)
		}
	}
	fastCheck.OnChanged = func(v bool) {
		if v {
			resilienceCheck.SetChecked(false)
		}
	}

	autostartCheck := widget.NewCheck("AutoStart", nil)
	autostartCheck.Checked = cfg.Autostart

	blackListEntry := widget.NewMultiLineEntry()
	blackListEntry.SetText(cfg.BlackList)
	blackListEntry.SetPlaceHolder("comma separated e.g. 1.1.1.1, 8.8.8.8/0, 192.168.1.0/24, example.com")
	blackListEntry.Wrapping = fyne.TextWrapWord

	fields := []fyne.Disableable{addrEntry, portEntry, userEntry, passEntry, httpPortEntry, socksPortEntry, resilienceCheck, fastCheck, autostartCheck, blackListEntry}

	// --- Log panel ---------------------------------------------------------
	var logMu sync.Mutex
	var allLines []string
	// A read-only multi-line Entry (not a Label) so the log is selectable and
	// copyable by the user.
	logEntry := widget.NewMultiLineEntry()
	logEntry.Wrapping = fyne.TextWrapWord
	logEntry.Disable() // read-only, but text stays selectable/copyable
	logScroll := container.NewVScroll(logEntry)

	refreshLog := func() {
		logMu.Lock()
		lines := append([]string{}, allLines...)
		logMu.Unlock()
		logEntry.SetText(strings.Join(lines, "\n"))
		logScroll.ScrollToBottom()
	}

	appendLog := func(line string) {
		logMu.Lock()
		allLines = append(allLines, line)
		logMu.Unlock()
		fyne.Do(refreshLog)
	}

	// --- Status bar --------------------------------------------------------
	statusDot := canvas.NewCircle(colorRed)
	statusDot.Resize(fyne.NewSize(14, 14))
	statusDot.FillColor = colorRed
	statusLabel := canvas.NewText("Disconnected", colorRed)
	statusLabel.TextStyle = fyne.TextStyle{Bold: true}
	pingLabel := widget.NewLabel("Ping: -")
	throughputLabel := widget.NewLabel("↑ 0 B/s  ↓ 0 B/s")
	statusBar := container.NewHBox(statusDot, statusLabel, layout.NewSpacer(), pingLabel, throughputLabel)

	// --- Start/Stop + Ping -------------------------------------------------
	startStopBtn := newActionButton("Start", nil)
	startStopBtn.Importance = widget.HighImportance
	pingBtn := widget.NewButton("Ping", func() {
		host := strings.TrimSpace(addrEntry.Text)
		p, e := strconv.Atoi(strings.TrimSpace(portEntry.Text))
		if host == "" || e != nil {
			fyne.Do(func() { pingLabel.SetText("Ping: -") })
			return
		}
		addr := net.JoinHostPort(host, strconv.Itoa(p))
		go func() {
			ms, perr := measurePing(addr)
			fyne.Do(func() {
				if perr != nil {
					pingLabel.SetText("Ping: fail")
				} else {
					pingLabel.SetText("Ping: " + strconv.FormatInt(ms, 10) + " ms")
				}
			})
		}()
	})

	var (
		engine  *tunnel.Engine
		socksLn net.Listener
		httpLn  net.Listener
		stopCh  chan struct{}
		running bool
	)

	setStatus := func(s tunnel.Status) {
		fyne.Do(func() {
			switch s {
			case tunnel.StatusConnected:
				statusDot.FillColor = colorGreen
				statusLabel.Text = "Connected"
				statusLabel.Color = colorGreen
			case tunnel.StatusRetrying:
				statusDot.FillColor = colorOrange
				statusLabel.Text = "Retrying"
				statusLabel.Color = colorOrange
			case tunnel.StatusDisconnected:
				statusDot.FillColor = colorRed
				statusLabel.Text = "Disconnected"
				statusLabel.Color = colorRed
			}
			statusDot.Refresh()
			statusLabel.Refresh()
		})
	}

	buildSettings := func() *config.Settings {
		port, _ := strconv.Atoi(strings.TrimSpace(portEntry.Text))
		httpPort, _ := strconv.Atoi(strings.TrimSpace(httpPortEntry.Text))
		socksPort, _ := strconv.Atoi(strings.TrimSpace(socksPortEntry.Text))
		return &config.Settings{
			Address:        strings.TrimSpace(addrEntry.Text),
			User:           strings.TrimSpace(userEntry.Text),
			Password:       passEntry.Text,
			Port:           port,
			HTTPProxyPort:  httpPort,
			SocksProxyPort: socksPort,
			ResilienceMode: resilienceCheck.Checked,
			FastReconnect:  fastCheck.Checked,
			BlackList:      blackListEntry.Text,
			Autostart:      autostartCheck.Checked,
		}
	}

	startProxy := func() {
		addr := strings.TrimSpace(addrEntry.Text)
		port, e1 := strconv.Atoi(strings.TrimSpace(portEntry.Text))
		user := strings.TrimSpace(userEntry.Text)
		pass := passEntry.Text
		httpPort, e2 := strconv.Atoi(strings.TrimSpace(httpPortEntry.Text))
		socksPort, e3 := strconv.Atoi(strings.TrimSpace(socksPortEntry.Text))
		if addr == "" || user == "" || pass == "" || e1 != nil || e2 != nil || e3 != nil {
			appendLog("Validation failed: fill all fields and use valid integer ports.")
			return
		}

		router := tunnel.NewRouter(buildSettings().BlackListEntries())

		save := buildSettings()
		if serr := config.Save(save); serr != nil {
			appendLog("Failed to save config: " + serr.Error())
			return
		}

		updateAddr()

		for _, f := range fields {
			f.Disable()
		}

		mode := tunnel.FastReconnect
		if save.ResilienceMode {
			mode = tunnel.NetworkResilience
		}
		logger := log.NewLogger(appendLog)
		engine = tunnel.NewEngine(net.JoinHostPort(addr, strconv.Itoa(port)), user, pass, mode, logger, setStatus)
		stopCh = make(chan struct{})
		go engine.Run(stopCh)
		var socksErr, httpErr error
		socksLn, socksErr = tunnel.StartSocksServer(socksPort, engine, router, logger)
		if socksErr != nil {
			appendLog("Failed to start SOCKS server: " + socksErr.Error())
		}
		httpLn, httpErr = tunnel.StartHTTPProxyServer(httpPort, engine, router, logger)
		if httpErr != nil {
			appendLog("Failed to start HTTP proxy server: " + httpErr.Error())
		}

		// If neither proxy could bind, there is nothing useful to run:
		// stop the engine, re-enable the form, and tell the user. Done on
		// the main thread so it overrides any pending "connected" callback.
		if socksErr != nil && httpErr != nil {
			if stopCh != nil {
				close(stopCh)
			}
			engine = nil
			fyne.Do(func() {
				for _, f := range fields {
					f.Enable()
				}
				setStatus(tunnel.StatusDisconnected)
				dialog.ShowError(fmt.Errorf("Could not start the proxy:\n%s\n%s", socksErr.Error(), httpErr.Error()), w)
			})
			return
		}
		// Partial failure: warn but keep the working proxy running.
		if socksErr != nil || httpErr != nil {
			var msg string
			if socksErr != nil {
				msg = socksErr.Error()
			} else {
				msg = httpErr.Error()
			}
			fyne.Do(func() {
				dialog.ShowInformation("Proxy warning", "One proxy failed to bind:\n"+msg, w)
			})
		}

		running = true
		startStopBtn.SetText("Stop")
		startStopBtn.Importance = widget.MediumImportance
		startStopBtn.Refresh()
	}

	stopProxy := func() {
		if socksLn != nil {
			_ = socksLn.Close()
		}
		if httpLn != nil {
			_ = httpLn.Close()
		}
		if stopCh != nil {
			close(stopCh)
		}
		engine = nil

		for _, f := range fields {
			f.Enable()
		}
		setStatus(tunnel.StatusDisconnected)
		running = false
		startStopBtn.SetText("Start")
		startStopBtn.Importance = widget.HighImportance
		startStopBtn.Refresh()
	}

	startStopBtn.OnTapped = func() {
		if !running {
			startProxy()
		} else {
			stopProxy()
		}
	}

	applyAutostart := func(v bool) {
		s := buildSettings()
		s.Autostart = v
		if serr := config.Save(s); serr != nil {
			appendLog("Failed to save config: " + serr.Error())
		}
		if aerr := configureAutostart(v); aerr != nil {
			appendLog("Autostart error: " + aerr.Error())
		} else if v {
			appendLog("AutoStart enabled.")
		} else {
			appendLog("AutoStart disabled.")
		}
	}

	autostartCheck.OnChanged = func(v bool) {
		if v {
			dialog.NewConfirm("Enable AutoStart",
				"This will launch Cipher Proxy (and start the proxy) automatically when you log in. Continue?",
				func(confirmed bool) {
					if !confirmed {
						autostartCheck.SetChecked(false) // revert, no change
						return
					}
					applyAutostart(true)
				}, w).Show()
			return
		}
		applyAutostart(false)
	}

	// --- Throughput ticker -------------------------------------------------
	var lastIn, lastOut int64
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			var cin, cout int64
			if engine != nil {
				cin = engine.Counters().SnapshotIn()
				cout = engine.Counters().SnapshotOut()
			}
			dIn := cin - lastIn
			dOut := cout - lastOut
			lastIn, lastOut = cin, cout
			rateIn := float64(dIn) / 0.5
			rateOut := float64(dOut) / 0.5
			fyne.Do(func() {
				throughputLabel.SetText("↑ " + formatRate(rateOut) + "  ↓ " + formatRate(rateIn))
			})
		}
	}()

	// --- Layout ------------------------------------------------------------
	settingsCol1 := widget.NewForm(
		widget.NewFormItem("Address", addrEntry),
		widget.NewFormItem("User", userEntry),
		widget.NewFormItem("Password", passEntry),
	)
	settingsCol2 := widget.NewForm(
		widget.NewFormItem("Port", portEntry),
		widget.NewFormItem("HTTP Proxy Port", httpPortEntry),
		widget.NewFormItem("Socks5 Proxy Port", socksPortEntry),
	)
	formGrid := container.NewGridWithColumns(2, settingsCol1, settingsCol2)

	addrGrid := container.NewGridWithColumns(2,
		container.NewBorder(nil, nil, nil, copyHTTP, httpAddrEntry),
		container.NewBorder(nil, nil, nil, copySocks, socksAddrEntry),
	)
	addrSection := container.NewVBox(
		widget.NewLabelWithStyle("Proxy Addresses", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		addrGrid,
	)

	controls := container.NewVBox(
		widget.NewLabelWithStyle("Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		formGrid,
		addrSection,
		container.NewHBox(resilienceCheck, fastCheck, autostartCheck),
		container.NewHBox(startStopBtn, layout.NewSpacer(), pingBtn),
		widget.NewLabelWithStyle("Black List Addresses (won't route these)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		blackListEntry,
		widget.NewLabelWithStyle("Logs", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

	content := container.NewBorder(controls, statusBar, nil, nil, logScroll)
	w.SetContent(content)

	w.Resize(fyne.NewSize(452, 625))
	w.ShowAndRun()

	// Auto-start the proxy on launch when "AutoStart" was enabled.
	if cfg.Autostart {
		startProxy()
	}
}


// the app launches (and, because Autostart is set, starts the proxy) on login.
func configureAutostart(enabled bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Join(os.Getenv("HOME"), ".config", "autostart")
	file := filepath.Join(dir, "cipherproxy.desktop")
	if !enabled {
		_ = os.Remove(file)
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	content := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=Cipher Proxy\n" +
		"Exec=" + exe + "\n" +
		"Comment=Cipher Proxy autostart (starts proxy automatically)\n" +
		"X-GNOME-Autostart-enabled=true\n"
	return os.WriteFile(file, []byte(content), 0600)
}

func formatRate(bps float64) string {
	switch {
	case bps >= 1<<20:
		return strconv.FormatFloat(bps/(1<<20), 'f', 1, 64) + " MB/s"
	case bps >= 1<<10:
		return strconv.FormatFloat(bps/(1<<10), 'f', 1, 64) + " KB/s"
	default:
		return strconv.FormatFloat(bps, 'f', 0, 64) + " B/s"
	}
}
