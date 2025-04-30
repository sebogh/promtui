package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sebogh/promtui/internal"
)

var (
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4"))

	infoStyle = titleStyle

	redStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
	greenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))
	boldStyle  = lipgloss.NewStyle().Bold(true)

	grayStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
)

type tickMsg time.Time

type sampledMsg struct {
	fetched bool
	error   error
}

type model struct {
	interval    time.Duration
	data        *internal.Store
	search      string
	ready       bool
	viewport    viewport.Model
	endpoint    string
	ticker      *time.Ticker
	stopped     bool
	showHistory bool
	showDerived bool
}

func main() {
	endpoint := flag.String("endpoint", "http://localhost:8080/healthz/metrics", "metrics endpoint")
	interval := flag.Duration("interval", 5*time.Second, "refresh interval (e.g., 10s, 1m)")
	search := flag.String("search", "", "metrics search filter")
	disableHistoryView := flag.Bool("disable-history", false, "disable history")
	disableDerivedView := flag.Bool("disable-derived", false, "disable derived metrics")
	help := flag.Bool("help", false, "show help")
	version := flag.Bool("version", false, "show version")

	flag.Parse()
	if *help {
		flag.Usage()
		os.Exit(0)
	}
	if *version {
		bi, ok := debug.ReadBuildInfo()
		if !ok {
			fmt.Println("Error reading Build Info")
			os.Exit(1)
		}
		var revision string
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				revision = s.Value
				break
			}
		}
		fmt.Printf("version: %s, revision: %s\n", bi.Main.Version, revision)
		os.Exit(0)
	}

	// For now, we only need 3 data-points to show the delta between the last two
	// values or last two rates.
	ts := internal.NewStore(3, *endpoint)
	if _, err := ts.Sample(); err != nil {
		fmt.Println("Error fetching initial metrics:", err)
		os.Exit(1)
	}

	m := &model{
		search:      *search,
		interval:    *interval,
		data:        ts,
		endpoint:    strings.TrimSpace(*endpoint),
		ticker:      time.NewTicker(*interval),
		showHistory: !*disableHistoryView,
		showDerived: !*disableDerivedView,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}

func (m *model) Init() tea.Cmd {
	return sleepCmd(m.ticker)
}

func (m *model) Update(teaMsg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := teaMsg.(type) {
	case sampledMsg:
		switch {
		case msg.error != nil:
			content := fmt.Sprintf("Error fetching metrics: %s", msg.error.Error())
			m.viewport.SetContent(content)
		case msg.fetched:
			m.metricsView()
		}
		if !m.stopped {
			m.ticker.Reset(m.interval)
			cmds = append(cmds, sleepCmd(m.ticker))
		}
	case tickMsg:
		m.ticker.Stop()
		cmds = append(cmds, sampleCmd(m.data))
	case tea.WindowSizeMsg:
		headerHeight := lipgloss.Height(m.headerView())
		footerHeight := lipgloss.Height(m.footerView())
		verticalMarginHeight := headerHeight + footerHeight
		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMarginHeight)
			m.viewport.YPosition = headerHeight
			m.metricsView()
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMarginHeight
		}
	case tea.KeyMsg:
		switch {
		case msg.String() == "ctrl+c":
			return m, tea.Quit
		case msg.String() == "ctrl+r":
			m.ticker.Stop()
			cmds = append(cmds, sampleCmd(m.data))
		case msg.String() == "ctrl+p":
			if m.stopped {
				cmds = append(cmds, sampleCmd(m.data))
			} else {
				m.ticker.Stop()
			}
			m.stopped = !m.stopped
		case msg.Type == tea.KeyBackspace:
			if len(m.search) > 0 {
				m.search = m.search[:len(m.search)-1]
			}
			m.metricsView()
		case msg.Type == tea.KeyRunes:
			for _, r := range msg.Runes {
				if unicode.IsLetter(r) || unicode.IsDigit(r) || msg.String() == "_" || msg.String() == "-" {
					m.search += string(r)
				}
			}
			m.metricsView()
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(teaMsg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *model) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}
	return fmt.Sprintf("%s\n%s\n%s", m.headerView(), m.viewport.View(), m.footerView())
}

func sleepCmd(t *time.Ticker) tea.Cmd {
	return func() tea.Msg {
		return tickMsg(<-t.C)
	}
}

func sampleCmd(ts *internal.Store) tea.Cmd {
	return func() tea.Msg {
		fetched, err := ts.Sample()
		if err != nil {
			return sampledMsg{error: err}
		}
		if !fetched {
			return sampledMsg{}
		}
		return sampledMsg{fetched: true}
	}
}

func (m *model) headerView() string {
	var title string
	if m.search != "" {
		title = titleStyle.Render("Search: " + m.search + " ")
	}
	var url string
	if m.stopped {
		url = titleStyle.Render(" paused - " + m.endpoint)
	} else {
		url = titleStyle.Render(" " + m.interval.String() + " - " + m.endpoint)
	}
	line := infoStyle.Render(strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(title)-lipgloss.Width(url))))
	return lipgloss.JoinHorizontal(lipgloss.Center, title, line, url)
}

func (m *model) footerView() string {
	info := infoStyle.Render(fmt.Sprintf(" %.f%%", m.viewport.ScrollPercent()*100))
	keys := infoStyle.Render("CTRL+c: quit | CTRL+r: refresh | CTRL+p: (un-)pause | <xyz>: search \"xyz\" ")
	line := infoStyle.Render(strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(info)-lipgloss.Width(keys))))
	return lipgloss.JoinHorizontal(lipgloss.Center, keys, line, info)
}

func (m *model) metricsView() {
	dump, err := m.data.Dump(m.search)
	maxWidthStyle := lipgloss.NewStyle().MaxWidth(m.viewport.Width)
	if err != nil {
		content := maxWidthStyle.Render(fmt.Sprintf("Error rendering metrics: %s", err.Error()))
		m.viewport.SetContent(content)
	}
	sb := strings.Builder{}
	for _, series := range dump {
		derived := m.derive(series)
		for _, d := range derived {
			if len(d) == 0 {
				continue
			}
			sb.WriteString(renderSeries(d, m.showHistory, m.showDerived, maxWidthStyle))
		}
	}
	content := sb.String()
	m.viewport.SetContent(content)
}

func computeRate(c, p internal.Observation) internal.Observation {
	dur := c.Time.Sub(p.Time)
	delta := c.Value - p.Value
	var rate float64
	if dur < time.Second {
		scale := float64(time.Second.Nanoseconds() / dur.Nanoseconds())
		rate = delta * scale
	} else {
		rate = delta / dur.Seconds()
	}
	return internal.NewObservation(rateName(c.Name), internal.ObservationCounterRate, c.Time, rate)
}

func (m *model) derive(ots []internal.Observation) [][]internal.Observation {
	var derived [][]internal.Observation
	derived = append(derived, ots)
	o := ots[0]

	// Derive a rate series from counter like items.
	if (o.Kind == internal.ObservationCounter || o.Kind == internal.ObservationHistogramCount) && len(ots) > 1 {
		rs := make([]internal.Observation, 0, len(ots))
		for i := 0; i < len(ots)-1; i++ {
			rs = append(rs, computeRate(ots[i], ots[i+1]))
		}
		derived = append(derived, rs)
	}
	return derived
}

func rateName(name string) string {
	split := strings.Split(name, " ")
	name = split[0] + "_per_second_rate"
	if len(split) > 1 {
		name += " " + strings.Join(split[1:], " ")
	}
	return name
}

func round(f float64) float64 {
	return math.Round(f*100) / 100
}

func format(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func isDerived(kind internal.ObservationKind) bool {
	return kind == internal.ObservationCounterRate || kind == internal.ObservationHistogramAvg
}

// renderSeries renders a single item series to a single line string.
func renderSeries(obs []internal.Observation, showHistory, showDerived bool, maxWidthStyle lipgloss.Style) string {

	o := obs[0]
	derived := isDerived(o.Kind)

	// If we have no labels, return the name.
	if !showDerived && derived {
		return ""
	}

	// Add a prefix for showDerived metrics.
	s := " "
	if derived {
		s = "+"
	}

	// If we have only one value, return name and value.
	cv := round(obs[0].Value)
	s += o.Name + " " + format(cv)
	if len(obs) < 2 {
		return maxWidthStyle.Render(s) + "\n"
	}

	// Get the previous value.
	pv := round(obs[1].Value)

	// If unchanged, return.
	if cv == pv {
		return maxWidthStyle.Render(s) + "\n"
	}

	// Changed values will be bold.
	s = boldStyle.Render(s)

	// add colored arrows to indicate the change.
	if cv > pv {
		s += redStyle.Render(" ⬆")
	} else {
		s += greenStyle.Render(" ⬇")
	}

	// If showHistory view is enabled, append the delta to the previous value.
	if showHistory {
		delta := round(math.Abs(cv - pv))
		if cv > pv {
			s += grayStyle.Render(" (+" + format(delta) + ")")
		} else {
			s += grayStyle.Render(" (-" + format(delta) + ")")
		}
	}
	return maxWidthStyle.Render(s) + "\n"
}
