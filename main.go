// ABeast is a menu bar app that shows where each AI agent's traffic leaves
// the proxy, alerts when that exit breaks or moves, and in protection mode
// holds the agents on their node, so that a node going down fails their
// requests instead of sending them out another way, and pauses the agents
// while the proxy cannot keep their traffic in.
package main

import (
	"context"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/10ef/ABeast/internal/agent"
	"github.com/10ef/ABeast/internal/config"
	"github.com/10ef/ABeast/internal/envcheck"
	"github.com/10ef/ABeast/internal/monitor"
)

type app struct {
	cfg        *config.Config
	mon        *monitor.Monitor
	tray       *mygo.Tray
	menu       *mygo.Menu
	iconState  trayState
	last       monitor.Snapshot
	simulating map[string]bool
	title      string // TrayTitle, changed from the menu
	env        envcheck.Report
	panel      panel
}

func (a *app) item(id string) *mygo.MenuItem { return a.menu.ItemByID(id) }

func notify(id, title, body string) {
	log.Printf("alert: %s: %s", title, body)
	if !mygo.NotificationsSupported() {
		return
	}
	n := mygo.NewNotification(mygo.NotificationOptions{ID: id, Title: title, Body: body, Group: "exitguard"})
	if err := n.Show(); err != nil {
		log.Println("notification:", err)
	}
}

func main() {
	mygo.App.SetName("ABeast")
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)

	cfg, err := config.Load()
	if err != nil {
		log.Println("config:", err, "- using defaults")
	}
	a := &app{cfg: cfg, simulating: map[string]bool{}, title: cfg.TrayTitle}
	a.mon = monitor.New(cfg)
	a.mon.SetGuard(agent.New())
	// ABEAST_SIMULATE=Codex starts with that route unsafe, for testing.
	// Keep the previous environment variable working for existing scripts.
	simulate := os.Getenv("ABEAST_SIMULATE")
	if simulate == "" {
		simulate = os.Getenv("EXITPIN_SIMULATE")
	}
	if simulate == "" {
		simulate = os.Getenv("EXITGUARD_SIMULATE")
	}
	for _, name := range strings.Split(simulate, ",") {
		if name = strings.TrimSpace(name); name != "" {
			a.simulating[name] = true
			a.mon.Simulate(name, true)
		}
	}
	a.mon.OnSnapshot = func(s monitor.Snapshot) { mygo.RunOnMain(func() { a.render(s) }) }
	a.mon.OnAlert = notify
	ctx, cancel := context.WithCancel(context.Background())

	mygo.App.WhenReady(func() {
		a.menu = a.buildMenu()
		icon, template := trayIcon(trayChecking)
		a.tray, err = mygo.NewTray(mygo.TrayOptions{
			Icon:           icon,
			IconIsTemplate: template,
			Title:          "…",
			ToolTip:        "ABeast · 正在检测出口…",
		})
		if err != nil {
			log.Fatal("tray: ", err)
		}
		if runtime.GOOS == "linux" {
			// AppIndicator only shows menus, and tells nothing of clicks.
			a.tray.SetMenu(a.menu)
		} else {
			// A click opens the panel; a right click the menu, with the
			// details and the advanced settings. The menu is set for that
			// popup only, as it was when last set, so that clicks keep
			// opening the panel.
			a.tray.OnClick(a.togglePanel)
			a.tray.OnRightClick(func() {
				if a.panel.win != nil {
					a.panel.win.Close()
				}
				a.tray.SetMenu(a.menu)
				defer a.tray.SetMenu(nil)
				a.tray.PopUpMenu()
			})
		}
		go a.checkEnv()
		mygo.Power.OnSuspend(a.mon.Sleep)
		mygo.Power.OnResume(a.mon.Wake)
		mygo.Network.OnChanged(func(mygo.NetworkStatus) { a.mon.Kick() })
		go a.mon.Run(ctx)
	})
	// Never leave an agent frozen, or a group pinned or rejecting, behind.
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		cancel()
		restore, done := context.WithTimeout(context.Background(), 3*time.Second)
		a.mon.Restore(restore)
		done()
		if a.tray != nil {
			a.tray.Destroy()
		}
	})
	// The panel is the only window: keep running as it closes.
	mygo.App.OnWindowAllClosed(func() {})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
