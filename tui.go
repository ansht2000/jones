package main

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type State int

const (
	Initial State = iota
	Accepting
	Running
)

const logo = `
     ____.
    |    | ____   ____   ____   ______
    |    |/  _ \ /    \_/ __ \ /  ___/
/\__|    (  <_> )   |  \  ___/ \___ \ 
\________|\____/|___|  /\___  >____  >
                     \/     \/     \/
`

type model struct {
	state State
	commands []string
	cursor int
	selected map[int]struct{}
	textInput  textinput.Model
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "help"
	ti.Width = 20
	ti.Focus()

	return model{
		state: Initial,
		commands: getCommands().getKeys(),
		selected: make(map[int]struct{}),
		textInput: ti,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch m.state {
	case Initial:
		switch msg.(type) {
		case tea.KeyMsg:
			m.state = Accepting
		}
	case Accepting:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.Type {
			case tea.KeyEscape, tea.KeyCtrlC:
				cmd = tea.Quit
			case tea.KeyEnter:
				m.state = Running
			default:
				m.textInput, cmd = m.textInput.Update(msg)
			}
		}
	case Running:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "ctrl+c", "q":
				cmd = tea.Quit
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.commands) - 1 {
					m.cursor++
				}
			case "enter", " ":
				_, ok := m.selected[m.cursor]
				if ok {
					delete(m.selected, m.cursor)
				} else {
					m.selected[m.cursor] = struct{}{}
				}
			}
		}
	}

	return m, cmd
}

func (m model) View() string {
	s := ""
	switch m.state {
	case Initial:
		s += logo
		s += "\n\n"
		s += "Press any key to continue..."
	case Accepting:
		s += "What would you like to do?\n"
		s += m.textInput.View()
	case Running:
		s += "\n\n"
		s += "Run a command!\n\n"

		for i, command := range m.commands {
			cursor := " "
			if m.cursor == i {
				cursor = ">"
			}

			s += fmt.Sprintf("%s %s\n", cursor, command)
		}

		s += "\nPress q to quit.\n"
	}

	return s
}