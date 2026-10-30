package main

import (
	"context"
	"fmt"
	"github.com/10ef/ABeast/internal/agent"
	"github.com/10ef/ABeast/internal/config"
	"github.com/10ef/ABeast/internal/envcheck"
	"github.com/10ef/ABeast/internal/monitor"
	"github.com/egoist/mygo"
	"log"
	"runtime"
	"strings"
)

func (a *app) buildMenu() *mygo.Menu {
	info := func(id, label string) *mygo.MenuItem {
		return &mygo.MenuItem{ID: id, Label: label, Disabled: true}
	}
	items := []*mygo.MenuItem{info("status", "正在检测出口…"), mygo.Separator()}
	for i, t := range a.cfg.Targets {
		items = append(items,
			&mygo.MenuItem{ID: fmt.Sprintf("r%d", i), Label: t.Name + "  检测中…", Click: a.copyRoute(i)},
			info(fmt.Sprintf("r%d-ip", i), "      "+t.Host),
			&mygo.MenuItem{ID: fmt.Sprintf("r%d-path", i), Disabled: true, Hidden: true},
			&mygo.MenuItem{ID: fmt.Sprintf("r%d-why", i), Disabled: true, Hidden: true},
		)
	}
	items = append(items, &mygo.MenuItem{ID: "leak-head", Disabled: true, Hidden: true})
	for i := range maxRisks {
		items = append(items, &mygo.MenuItem{ID: fmt.Sprintf("leak-%d", i), Disabled: true, Hidden: true})
	}
	items = append(items,
		&mygo.MenuItem{ID: "env", Label: "环境检测：检测中…", Submenu: a.envMenu()},
		mygo.Separator(),
		&mygo.MenuItem{ID: "protect", Label: "🔒 保护模式（锁定节点 · 断开异常连接）", Type: mygo.MenuItemCheckbox,
			Checked: a.cfg.Protect, Click: func(it *mygo.MenuItem, _ *mygo.Window) {
				a.mon.SetProtect(it.IsChecked())
			}},
		&mygo.MenuItem{ID: "protect-state", Disabled: true, Hidden: true},
		&mygo.MenuItem{ID: "protect-last", Disabled: true, Hidden: true},
		&mygo.MenuItem{ID: "hold-state", Disabled: true, Hidden: true},
		&mygo.MenuItem{ID: "resume", Label: "立即恢复 Agent（关闭保护模式）", Hidden: true,
			Click: func(*mygo.MenuItem, *mygo.Window) { a.mon.ResumeAgents() }},
		&mygo.MenuItem{ID: "repin", Label: "换节点（解锁后重新选择）", Hidden: !a.cfg.Protect,
			Click: func(*mygo.MenuItem, *mygo.Window) { go a.mon.Repin(context.Background()) }},
		mygo.Separator(),
		&mygo.MenuItem{Label: "立即检测", Accelerator: "CmdOrCtrl+R", Click: func(*mygo.MenuItem, *mygo.Window) { a.recheck() }},
		&mygo.MenuItem{Label: "高级", Submenu: a.advancedMenu()},
		&mygo.MenuItem{ID: "login", Label: "开机启动", Type: mygo.MenuItemCheckbox, Checked: mygo.App.OpenAtLogin(),
			Click: func(it *mygo.MenuItem, _ *mygo.Window) {
				if err := mygo.App.SetOpenAtLogin(it.IsChecked()); err != nil {
					log.Println("open at login:", err)
					it.SetChecked(!it.IsChecked())
				}
			}},
		mygo.Separator(),
		&mygo.MenuItem{Role: mygo.RoleQuit, Label: "退出 ABeast"},
	)
	return mygo.NewMenu(items)
}

// advancedMenu holds the details and the protections that need setting up
// or freeze programs, out of the way of protection mode.
func (a *app) advancedMenu() []*mygo.MenuItem {
	info := func(id, label string) *mygo.MenuItem {
		return &mygo.MenuItem{ID: id, Label: label, Disabled: true}
	}
	items := []*mygo.MenuItem{info("mihomo", "Clash API：检测中…")}
	if a.cfg.ShowDirect {
		items = append(items, info("direct", "直连（不走代理的程序）  检测中…"))
	}
	items = append(items, info("procs", "Agent 进程：检测中…"))
	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{Label: "把当前出口设为正常基线", Click: func(*mygo.MenuItem, *mygo.Window) { a.mon.AcceptCurrent() }},
	)
	if runtime.GOOS == "darwin" { // a Windows tray icon shows no text
		items = append(items, &mygo.MenuItem{Label: "菜单栏显示", Submenu: a.titleMenu()})
	}
	var simulate []*mygo.MenuItem
	for _, t := range a.cfg.Targets {
		simulate = append(simulate, &mygo.MenuItem{ID: "sim-" + t.Name, Label: "模拟 " + t.Name + " 出口异常",
			Type: mygo.MenuItemCheckbox, Checked: a.simulating[t.Name],
			Click: func(it *mygo.MenuItem, _ *mygo.Window) { a.mon.Simulate(t.Name, it.IsChecked()) }})
	}
	return append(items,
		&mygo.MenuItem{Label: "测试", Submenu: simulate},
		&mygo.MenuItem{Label: "打开配置文件（重启后生效）", Click: func(*mygo.MenuItem, *mygo.Window) {
			if err := mygo.Shell.OpenPath(config.Path()); err != nil {
				log.Println("open config:", err)
			}
		}},
	)
}

// envMenu lists the environment check's signals, after fuck-claude.com.
func (a *app) envMenu() []*mygo.MenuItem {
	var items []*mygo.MenuItem
	for i := range envSignals {
		items = append(items, &mygo.MenuItem{ID: fmt.Sprintf("env-%d", i), Disabled: true})
	}
	return append(items,
		&mygo.MenuItem{ID: "env-note", Label: "仅读取本机时区与语言设置，不上传", Disabled: true},
		mygo.Separator(),
		&mygo.MenuItem{Label: "重新检测", Click: func(*mygo.MenuItem, *mygo.Window) { go a.checkEnv() }},
	)
}

// envSignals is how many signals the environment check scores.
const envSignals = 4

// checkEnv runs the environment check and shows it.
func (a *app) checkEnv() {
	r := envcheck.Check()
	mygo.RunOnMain(func() {
		a.env = r
		a.renderEnv(r)
		a.refreshPanel()
	})
}

// renderEnv shows an environment check. It runs on the main thread.
func (a *app) renderEnv(r envcheck.Report) {
	a.item("env").SetLabel(envLine(r))
	for i := range envSignals {
		it := a.item(fmt.Sprintf("env-%d", i))
		if i >= len(r.Signals) {
			it.SetVisible(false)
			continue
		}
		it.SetLabel(signalLine(r.Signals[i]))
		it.SetVisible(true)
	}
}

// titleMenu offers the presets, plus the config's own template when it is
// none of them.
func (a *app) titleMenu() []*mygo.MenuItem {
	choose := func(tpl string) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			a.title = tpl
			a.mon.SetTrayTitle(tpl)
			a.tray.SetTitle(trayTitle(a.last, tpl))
		}
	}
	var items []*mygo.MenuItem
	custom := true
	for _, p := range titlePresets {
		custom = custom && p.tpl != a.title
		items = append(items, &mygo.MenuItem{Label: p.label, Type: mygo.MenuItemRadio,
			Checked: p.tpl == a.title, Click: choose(p.tpl)})
	}
	if custom {
		items = append(items, &mygo.MenuItem{Label: "自定义：" + a.title, Type: mygo.MenuItemRadio,
			Checked: true, Click: choose(a.title)})
	}
	return items
}

// copyRoute copies a route's exit to the clipboard.
func (a *app) copyRoute(i int) func(*mygo.MenuItem, *mygo.Window) {
	return func(*mygo.MenuItem, *mygo.Window) {
		if i < len(a.last.Routes) {
			r := a.last.Routes[i]
			mygo.Clipboard.WriteText(fmt.Sprintf("%s %s %s %s %s", r.Target.Name, r.Trace.IP, r.Trace.Country, place(r.Geo), r.Geo.ISP))
		}
	}
}

// render shows a snapshot. It runs on the main thread.
func (a *app) render(s monitor.Snapshot) {
	a.last = s
	currentIcon := stateForTray(s)
	if currentIcon != a.iconState {
		icon, template := trayIcon(currentIcon)
		if err := a.tray.SetIcon(icon, template); err != nil {
			log.Println("tray icon:", err)
		} else {
			a.iconState = currentIcon
		}
	}
	a.tray.SetTitle(trayTitle(s, a.title))
	a.tray.SetToolTip(tooltip(s))

	status := currentIcon.label(s)
	a.item("status").SetLabel(status + "  ·  " + s.Checked.Format("15:04:05"))

	for i, r := range s.Routes {
		a.item(fmt.Sprintf("r%d", i)).SetLabel(routeLine(r))
		a.item(fmt.Sprintf("r%d-ip", i)).SetLabel("      " + routeDetail(r))
		// The group the blocker switches, only where the override script set
		// it up: without it the line would only say the group is missing.
		path := a.item(fmt.Sprintf("r%d-path", i))
		st := s.Block.Routes[r.Target.Name]
		path.SetLabel("      " + routePolicy(st))
		path.SetVisible(s.Protect && s.Block.Available && r.Target.BlockGroup != "" && st.Err == "")
		why := a.item(fmt.Sprintf("r%d-why", i))
		why.SetLabel("      ⚠️ " + r.Problem)
		why.SetVisible(r.Problem != "")
	}
	a.renderRisks(s)

	a.item("protect").SetChecked(s.Protect)
	state, last := "", ""
	if s.Protect {
		state = "      " + pinLine(s)
		switch {
		case s.Watch.Err != "":
			last = "      ⚠️ " + s.Watch.Err
		case s.Watch.Closed > 0:
			last = fmt.Sprintf("      已断开 %d 条连接，最近：%s", s.Watch.Closed, s.Watch.Last)
		}
	}
	a.item("protect-state").SetLabel(state)
	a.item("protect-state").SetVisible(state != "")
	a.item("protect-last").SetLabel(last)
	a.item("protect-last").SetVisible(last != "")
	a.item("repin").SetVisible(s.Protect)
	paused := 0
	for _, n := range s.Guard.Paused {
		paused += n
	}
	hold := ""
	if s.Hold != "" {
		hold = "      ⏸ 已拦住 Agent：" + s.Hold
		if paused > 0 {
			hold += fmt.Sprintf("（已暂停 %d 个进程，恢复后自动继续）", paused)
		}
	}
	a.item("hold-state").SetLabel(hold)
	a.item("hold-state").SetVisible(hold != "")
	a.item("resume").SetVisible(paused > 0)

	if s.Direct != nil {
		a.item("direct").SetLabel(routeLine(*s.Direct) + "  " + s.Direct.Trace.IP)
	}
	mihomo := "Clash API：" + s.Block.Mode + " 模式"
	switch {
	case !a.mon.HasController():
		mihomo = "Clash API：未配置"
	case !s.Block.Available:
		mihomo = "Clash API：" + s.Block.Err
	case s.Block.Err != "":
		mihomo += "  ⚠️ " + s.Block.Err
	}
	a.item("mihomo").SetLabel(mihomo)

	var parts []string
	for _, name := range a.cfg.Agents() {
		p := fmt.Sprintf("%s ×%d", name, s.Guard.Running[agent.NormName(name)])
		if n := s.Guard.Paused[agent.NormName(name)]; n > 0 {
			p += fmt.Sprintf("（暂停 %d）", n)
		}
		parts = append(parts, p)
	}
	procs := "Agent 进程：" + strings.Join(parts, " · ")
	if len(s.Guard.Errors) > 0 {
		procs += "  ⚠️ " + s.Guard.Errors[0]
	}
	a.item("procs").SetLabel(procs)
	a.refreshPanel()
}

// maxRisks is how many risks of the self-check the menu lists.
const maxRisks = 5

// renderRisks lists what the self-check found, and nothing when it found
// nothing.
func (a *app) renderRisks(s monitor.Snapshot) {
	high := 0
	for _, r := range s.Risks {
		if r.High {
			high++
		}
	}
	head := ""
	switch {
	case high > 0:
		head = fmt.Sprintf("⚠️ 防泄漏：%d 项风险", high)
		if n := len(s.Risks) - high; n > 0 {
			head += fmt.Sprintf("，%d 条提示", n)
		}
	case len(s.Risks) > 0:
		head = fmt.Sprintf("防泄漏：%d 条提示", len(s.Risks))
	}
	a.item("leak-head").SetLabel(head)
	a.item("leak-head").SetVisible(head != "")
	for i := range maxRisks {
		it := a.item(fmt.Sprintf("leak-%d", i))
		if i >= len(s.Risks) {
			it.SetVisible(false)
			continue
		}
		mark := "      · "
		if s.Risks[i].High {
			mark = "      ⚠️ "
		}
		it.SetLabel(mark + s.Risks[i].Text)
		it.SetVisible(true)
	}
}

// pinLine shows the pinned nodes, once per group: "已锁定 🌏 ChatGPT/Claude →
// 日本 02", or why a route is not pinned.
func pinLine(s monitor.Snapshot) string {
	var parts []string
	seen := map[string]bool{}
	for _, r := range s.Routes {
		st := s.Pin.Routes[r.Target.Name]
		var p string
		switch {
		case st.Err != "":
			p = r.Target.Name + "：" + st.Err
		case st.Node == "" || seen[st.Group]:
			continue
		default:
			p = "已锁定 " + st.Group + " → " + st.Node
			if st.Fixed {
				p += "（自动测速组：节点失效时仍会切换）"
			}
		}
		seen[st.Group] = true
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return "等待检测后锁定…"
	}
	return strings.Join(parts, "；")
}

// routePolicy shows a route's group down to its node, then the agents'
// own connections: "🛡 EG-OpenAI → 🌏 ChatGPT/Claude → 日本 03 · codex ×2 → 日本 03".
func routePolicy(st monitor.BlockState) string {
	if st.Err != "" {
		return "策略：" + st.Group + "  ⚠️ " + st.Err
	}
	line := "策略：" + strings.Join(st.Path, " → ")
	if st.Blocked {
		line = "⛔️ 已阻断  " + line
	}
	if len(st.Agents) > 0 {
		line += " · " + strings.Join(st.Agents, " · ")
	}
	return line
}
