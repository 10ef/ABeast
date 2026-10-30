package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/10ef/ABeast/internal/envcheck"
	"github.com/10ef/ABeast/internal/monitor"
	"github.com/10ef/ABeast/internal/probe"
)

// The panel the icon opens: cards for the routes and the environment check,
// then the switches, in the manner of a native menu bar app. The menu stays
// for the right click, with the details and the advanced settings.
const (
	panelWidth  = 360
	panelHeight = 640
)

// panel holds what the panel's controls change. Like the rest of app, it is
// read and changed on the main thread only.
type panel struct {
	win     *mygo.Window // the open panel, or nil
	protect bool
	login   bool
	envOpen bool
}

// togglePanel opens the panel below the icon, or closes it.
func (a *app) togglePanel() {
	if a.panel.win != nil {
		a.panel.win.Close()
		return
	}
	a.panel.protect = a.last.Protect
	a.panel.login = mygo.App.OpenAtLogin()
	a.panel.win = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      a.tray,
		Placement: mygo.PlacementBottom,
		Gap:       4,
		Width:     panelWidth,
		Height:    panelHeight,
		Focusable: true,
		Shadow:    true,
		Popover:   true,
		Content:   ui.View(a.view),
	})
	a.panel.win.OnClosed(func() { a.panel.win = nil })
}

// refreshPanel draws the open panel again, after a snapshot or a check.
func (a *app) refreshPanel() {
	if a.panel.win != nil {
		a.panel.win.Invalidate()
	}
}

func (a *app) view(c *ui.Context) {
	t := c.Theme()
	s := a.last
	c.OnShortcut(ui.Cmd, ui.KeyQ, mygo.App.Quit)
	c.OnShortcut(ui.Cmd, ui.KeyR, a.recheck)
	c.Root().Background(ui.Transparent)
	box := ui.Column(c).Fill()
	switch {
	case c.Vibrancy(): // over the popover's material on macOS
	case runtime.GOOS == "windows": // which rounds and outlines the flyout
		box.Background(t.Background)
	default:
		box.Background(t.Background).Radius(12).Border(1, t.Border)
	}
	box.Children(func() {
		ui.Row(c).Padding(14, 16, 6, 16).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "ABeast").FontSize(15).Bold().Grow(1)
			checked := "检测中…"
			if !s.Checked.IsZero() {
				checked = s.Checked.Format("15:04:05")
			}
			ui.Text(c, checked).FontSize(12).TextColor(t.TextMuted)
		})
		ui.Scroll(c).Grow(1).Padding(6, 12, 12, 12).Gap(10).Children(func() {
			a.statusCard(c, s)
			for i, r := range s.Routes {
				a.routeCard(c.Key(fmt.Sprint("route", i)), i, r, s)
			}
			a.envCard(c, a.env)
			a.risks(c, s)
			a.protection(c, s)
			ui.Divider(c).Margin(4, 4)
			a.general(c)
		})
	})
}

// card is the box the panel groups things in. An alert outlines it in the
// alert's color.
func card(c *ui.Context, outline ui.Color) ui.Element {
	t := c.Theme()
	e := ui.Column(c).Padding(12, 14).Gap(4).Radius(10).Background(t.Surface)
	if outline != (ui.Color{}) {
		return e.Border(1.5, outline)
	}
	return e.Border(1, t.Border)
}

// dot is a small round light.
func dot(c *ui.Context, col ui.Color) {
	ui.Box(c).Size(8, 8).Radius(4).Shrink(0).Background(col)
}

func (a *app) stateColor(c *ui.Context, st trayState) ui.Color {
	t := c.Theme()
	switch st {
	case trayWarning:
		return t.Danger
	case trayPaused, trayRecovering:
		return t.Warning
	case trayChecking:
		return t.TextMuted
	}
	return t.Success
}

func (a *app) statusCard(c *ui.Context, s monitor.Snapshot) {
	st := stateForTray(s)
	col := a.stateColor(c, st)
	var outline ui.Color
	if st == trayWarning {
		outline = col
	}
	card(c, outline).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			dot(c, col)
			ui.Text(c, st.label(s)).FontSize(14).Bold().Grow(1)
		})
		if s.Hold != "" {
			ui.Text(c, "已拦住 Agent："+s.Hold).FontSize(12).TextColor(c.Theme().Warning)
		}
	})
}

func (a *app) routeCard(c *ui.Context, i int, r monitor.Route, s monitor.Snapshot) {
	t := c.Theme()
	var outline ui.Color
	if r.Unsafe {
		outline = t.Danger
	}
	e := card(c, outline).Tooltip("点击复制出口信息")
	e.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, r.Target.Name).FontSize(14).Bold()
			loc := "检测中…"
			switch {
			case r.Err != "":
				loc = "✕ " + r.Err
			case r.Trace.IP != "":
				loc = strings.TrimSpace(probeLoc(r))
			}
			ui.Text(c, loc).FontSize(13).Grow(1).MaxLines(1)
			if r.Trace.IP != "" {
				ui.Textf(c, "%dms", r.Trace.Latency.Milliseconds()).FontSize(12).TextColor(t.TextMuted)
			}
		})
		ui.Text(c, routeDetail(r)).FontSize(11).TextColor(t.TextMuted)
		if s.Protect {
			if st := s.Pin.Routes[r.Target.Name]; st.Node != "" {
				ui.Text(c, "已锁定 "+st.Group+" → "+st.Node).FontSize(11).TextColor(t.TextMuted)
			}
		}
		if r.Problem != "" {
			ui.Text(c, "⚠️ "+r.Problem).FontSize(12).TextColor(t.Danger)
		}
	})
	if e.Clicked() {
		a.copyRoute(i)(nil, nil)
	}
}

// probeLoc is where a route leaves: its flag and place.
func probeLoc(r monitor.Route) string {
	loc := place(r.Geo)
	if strings.TrimSpace(loc) == "" {
		loc = r.Trace.Country
	}
	return probe.FlagBefore(r.Trace.Country) + loc
}

func (a *app) envCard(c *ui.Context, r envcheck.Report) {
	t := c.Theme()
	card(c, ui.Color{}).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "环境检测").FontSize(14).Bold().Grow(1)
			if ui.Button(c.Key("env-again"), "重新检测").Clicked() {
				go a.checkEnv()
			}
		})
		if r.Checked.IsZero() {
			ui.Text(c, "检测中…").FontSize(12).TextColor(t.TextMuted)
			return
		}
		col := map[string]ui.Color{"low": t.Success, "medium": t.Warning, "high": t.Danger}[r.Band]
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Textf(c, "%d", r.Total).FontSize(30).Bold().TextColor(col)
			ui.Text(c, "/ 100").FontSize(12).TextColor(t.TextMuted)
			ui.Spacer(c)
			ui.Text(c, r.BandName()).FontSize(12).Bold().TextColor(col).
				Padding(3, 10).Radius(10).Background(col.Alpha(0.15))
		})
		ui.Meter(c, float64(r.Total), 0, 100, &ui.MeterLevels{Warning: 31, Critical: 61}).Label("环境检测分数")
		var hits []string
		for _, sig := range r.Signals {
			if sig.Hit() {
				hits = append(hits, sig.Name)
			}
		}
		if len(hits) == 0 {
			ui.Text(c, "没有命中明显的中国化信号").FontSize(12).TextColor(t.TextMuted)
		} else {
			ui.Text(c, "命中："+strings.Join(hits, "、")).FontSize(12).TextColor(t.TextMuted)
		}
		ui.Collapsible(c.Key("env-details"), "详情", &a.panel.envOpen, func() {
			for _, sig := range r.Signals {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					col := t.Success
					if sig.Hit() {
						col = map[bool]ui.Color{true: t.Danger, false: t.Warning}[sig.Score >= 0.6]
					}
					dot(c, col)
					ui.Text(c, sig.Name).FontSize(12).Width(64)
					ui.Text(c, sig.Raw).FontSize(12).TextColor(t.TextMuted).Grow(1).MaxLines(1)
					ui.Textf(c, "+%d/%d", sig.Points, sig.Weight).FontSize(11).TextColor(t.TextMuted)
				})
			}
			ui.Text(c, "仅读取本机时区与语言设置，不上传").FontSize(11).TextColor(t.TextMuted)
		})
	})
}

func (a *app) risks(c *ui.Context, s monitor.Snapshot) {
	if len(s.Risks) == 0 {
		return
	}
	t := c.Theme()
	section(c, "防泄漏")
	for _, r := range s.Risks {
		col := t.TextMuted
		if r.High {
			col = t.Danger
		}
		ui.Row(c).Gap(8).Padding(0, 4).AlignItems(ui.Center).Children(func() {
			dot(c, col)
			ui.Text(c, r.Text).FontSize(12).Grow(1)
		})
	}
}

// section heads a group of settings, as SAFETY does in macOS's panels.
func section(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(11).Bold().LetterSpacing(0.6).TextColor(c.Theme().TextMuted).Margin(6, 4, 0, 4)
}

// setting is a row with a title, a muted line below it and, at the end,
// what control builds.
func setting(c *ui.Context, title, desc string, control func()) {
	ui.Row(c).Gap(12).Padding(2, 4).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(13)
			if desc != "" {
				ui.Text(c, desc).FontSize(11).TextColor(c.Theme().TextMuted)
			}
		})
		control()
	})
}

func (a *app) protection(c *ui.Context, s monitor.Snapshot) {
	t := c.Theme()
	section(c, "保护")
	a.panel.protect = s.Protect
	setting(c, "保护模式", "锁定节点 · 断开异常连接", func() {
		if ui.Switch(c.Key("protect"), &a.panel.protect).Label("保护模式").Changed() {
			a.mon.SetProtect(a.panel.protect)
		}
	})
	if !s.Protect {
		return
	}
	ui.Column(c).Gap(3).Padding(0, 4).Children(func() {
		ui.Text(c, pinLine(s)).FontSize(11).TextColor(t.TextMuted)
		switch {
		case s.Watch.Err != "":
			ui.Text(c, "⚠️ "+s.Watch.Err).FontSize(11).TextColor(t.Danger)
		case s.Watch.Closed > 0:
			ui.Textf(c, "已断开 %d 条连接，最近：%s", s.Watch.Closed, s.Watch.Last).FontSize(11).TextColor(t.TextMuted)
		}
	})
	paused := 0
	for _, n := range s.Guard.Paused {
		paused += n
	}
	ui.Row(c).Gap(8).Padding(2, 4).Children(func() {
		if ui.Button(c.Key("repin"), "换节点").Tooltip("解锁后重新选择").Clicked() {
			go a.mon.Repin(context.Background())
		}
		if paused > 0 && ui.Button(c.Key("resume"), fmt.Sprintf("立即恢复 Agent（%d）", paused)).Tooltip("同时关闭保护模式").Clicked() {
			a.mon.ResumeAgents()
		}
	})
}

func (a *app) general(c *ui.Context) {
	t := c.Theme()
	setting(c, "开机启动", "", func() {
		if ui.Switch(c.Key("login"), &a.panel.login).Label("开机启动").Changed() {
			if err := mygo.App.SetOpenAtLogin(a.panel.login); err != nil {
				a.panel.login = !a.panel.login
			}
			a.item("login").SetChecked(a.panel.login)
		}
	})
	shortcut := func(label, keys string, fn func()) {
		row := ui.Row(c.Key(label)).Padding(6, 4).Radius(6).AlignItems(ui.Center)
		if row.Hovered() {
			row.Background(t.SurfaceHover)
		}
		row.Children(func() {
			ui.Text(c, label).FontSize(13).Grow(1)
			ui.Text(c, keys).FontSize(12).TextColor(t.TextMuted)
		})
		if row.Clicked() {
			fn()
		}
	}
	cmd := "⌘"
	if runtime.GOOS != "darwin" {
		cmd = "Ctrl+"
	}
	shortcut("立即检测", cmd+"R", a.recheck)
	ui.Text(c, "右键图标：高级设置与更多详情").FontSize(11).TextColor(t.TextMuted).Padding(0, 4)
	ui.Divider(c).Margin(4, 4)
	shortcut("退出 ABeast", cmd+"Q", mygo.App.Quit)
}

// recheck probes the routes and checks the environment again.
func (a *app) recheck() {
	a.mon.Kick()
	go a.checkEnv()
}
