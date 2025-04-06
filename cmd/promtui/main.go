package main

import (
	"flag"
	"fmt"
	"os"
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
)

type tickMsg time.Time

type model struct {
	interval time.Duration
	err      error
	data     *internal.History
	search   string
	ready    bool
	viewport viewport.Model
	endpoint string
}

func (m *model) Init() tea.Cmd {
	return sleepCmd(m.interval)
}

func (m model) headerView() string {
	var title string
	if m.search != "" {
		title = titleStyle.Render("Search: " + m.search)
	}
	url := titleStyle.Render(m.interval.String() + " - " + m.endpoint)
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(title)-lipgloss.Width(url)))
	return lipgloss.JoinHorizontal(lipgloss.Center, title, line, url)
}

func (m model) footerView() string {
	info := infoStyle.Render(fmt.Sprintf("%3.f%%", m.viewport.ScrollPercent()*100))
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(info)))
	return lipgloss.JoinHorizontal(lipgloss.Center, line, info)
}

func (m *model) Update(teaMsg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := teaMsg.(type) {
	case tickMsg:
		m.err = m.data.Fetch()
		if m.err == nil {
			content := m.data.Render(m.search)
			m.viewport.SetContent(content)
		}
		cmds = append(cmds, sleepCmd(m.interval))
	case tea.WindowSizeMsg:
		headerHeight := lipgloss.Height(m.headerView())
		footerHeight := lipgloss.Height(m.footerView())
		verticalMarginHeight := headerHeight + footerHeight
		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMarginHeight)
			m.viewport.YPosition = headerHeight
			content := m.data.Render(m.search)
			m.viewport.SetContent(content)
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
			m.err = m.data.Fetch()
			if m.err == nil {
				content := m.data.Render(m.search)
				m.viewport.SetContent(content)
			} else {
				content := fmt.Sprintf("Error fetching metrics: %s", m.err.Error())
				m.viewport.SetContent(content)
			}
		case msg.Type == tea.KeyBackspace:
			if len(m.search) > 0 {
				m.search = m.search[:len(m.search)-1]
			}
			content := m.data.Render(m.search)
			m.viewport.SetContent(content)
		case msg.Type == tea.KeyRunes:
			for _, r := range msg.Runes {
				if unicode.IsLetter(r) {
					m.search += string(r)
				}
				content := m.data.Render(m.search)
				m.viewport.SetContent(content)
			}
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

func sleepCmd(i time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(i)
		return tickMsg(time.Now())
	}
}

func main() {
	endpoint := flag.String("endpoint", "http://localhost:8080/healthz/metrics", "metrics endpoint")
	interval := flag.Duration("interval", 10*time.Second, "refresh interval (e.g., 10s, 1m)")
	bufferSize := flag.Int("buffer-size", 10, "size of the ring buffer")
	search := flag.String("search", "", "metrics search filter")
	help := flag.Bool("help", false, "show help")

	flag.Parse()
	if *help {
		flag.Usage()
		os.Exit(0)
	}

	history := internal.NewHistory(*bufferSize, *endpoint)
	if err := history.Fetch(); err != nil {
		fmt.Println("Error fetching initial metrics:", err)
		os.Exit(1)
	}

	m := &model{
		search:   *search,
		interval: *interval,
		data:     history,
		endpoint: strings.TrimSpace(*endpoint),
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
