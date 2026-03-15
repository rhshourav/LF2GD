// tui.go
package ui

import (
	"downloader/core" // Added
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type model struct {
	jobs []*core.FileJob // Store jobs to display them
}

func (m model) Init() tea.Cmd {
	return tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
		return t
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyMsg: // Allow quitting
		return m, tea.Quit
	case time.Time:
		return m, tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
			return t
		})
	}
	return m, nil
}

func (m model) View() string {
	s := "--- Downloader Running (Press q to quit) ---\n\n"
	for _, job := range m.jobs {
		s += fmt.Sprintf("[%s] -> %s\n", job.Name, job.URL)
	}
	return s
}

func StartUI(jobs []*core.FileJob, refresh int) {
	p := tea.NewProgram(model{jobs: jobs}) // Pass the jobs
	if _, err := p.Run(); err != nil {     // Use p.Run() instead of p.Start()
		fmt.Printf("Error running UI: %v", err)
	}
}
