package main

import (
	"flag"
	"fmt"
	"math"
	"os"
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
)

// tickMsg is a message returned from the ticker.
type tickMsg time.Time

// sampledMsg is a message returned from sampleCmd.
type sampledMsg struct {

	// fetched indicates if new metrics were fetched. If false, no need to update the
	// viewport.
	fetched bool

	// error is the error returned from the sample command. If nil, no error occurred.
	error error
}
type model struct {
	interval time.Duration
	data     *internal.TimeSeries
	search   string
	ready    bool
	viewport viewport.Model
	endpoint string
	ticker   *time.Ticker
	stopped  bool
}

func main() {
	endpoint := flag.String("endpoint", "http://localhost:8080/healthz/metrics", "metrics endpoint")
	interval := flag.Duration("interval", 5*time.Second, "refresh interval (e.g., 10s, 1m)")
	bufferSize := flag.Int("buffer-size", 10, "size of the ring buffer")
	search := flag.String("search", "", "metrics search filter")
	help := flag.Bool("help", false, "show help")

	flag.Parse()
	if *help {
		flag.Usage()
		os.Exit(0)
	}

	ts := internal.NewTimeSeries(*bufferSize, *endpoint)
	if _, err := ts.Sample(); err != nil {
		fmt.Println("Error fetching initial metrics:", err)
		os.Exit(1)
	}

	m := &model{
		search:   *search,
		interval: *interval,
		data:     ts,
		endpoint: strings.TrimSpace(*endpoint),
		ticker:   time.NewTicker(*interval),
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
				if unicode.IsLetter(r) {
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

func sampleCmd(ts *internal.TimeSeries) tea.Cmd {
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
	if err != nil {
		content := fmt.Sprintf("Error rendering metrics: %s", err.Error())
		m.viewport.SetContent(content)
	}
	sb := strings.Builder{}
	for _, item := range dump {
		renderItemSeries(&sb, item)
	}
	content := sb.String()
	m.viewport.SetContent(content)
}

func renderItemSeries(sb *strings.Builder, is internal.ItemSeries) {
	if len(is.Values) == 0 {
		return
	}
	cv := math.Round(is.Values[0]*100) / 100
	nameValue := is.Name + " " + strconv.FormatFloat(cv, 'f', -1, 64)
	if len(is.Values) < 2 {
		sb.WriteString(nameValue + "\n")
		return
	}
	pv := math.Round(is.Values[1]*100) / 100
	if cv > pv {
		sb.WriteString(boldStyle.Render(nameValue))
		sb.WriteString(redStyle.Render(" ⬆"))
	} else if cv < pv {
		sb.WriteString(boldStyle.Render(nameValue))
		sb.WriteString(greenStyle.Render(" ⬇"))
	} else {
		sb.WriteString(nameValue + "")
	}
	sb.WriteString("\n")
}
