package main

import (
	// "fmt"

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

type Model struct {
	state State
	commands CommandMap
	text_input  textinput.Model
	command_return_text string
}

func initialModel() Model {
	ti := textinput.New()
	ti.Width = 20
	ti.Focus()

	return Model{
		state: Initial,
		commands: getCommands(),
		text_input: ti,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
				cmd = sendCommandMsg(m.text_input.Value())
				m.text_input.SetValue("")
			default:
				m.text_input, cmd = m.text_input.Update(msg)
			}
		case CommandMsg:
			command, ok := m.commands[string(msg)]
			if !ok {
				m.command_return_text = "Invalid command\n"
			} else {
				m.command_return_text = command.callback(&m)
			}
		}
	}

	return m, cmd
}

func (m Model) View() string {
	s := ""
	switch m.state {
	case Initial:
		s += logo
		s += "\n\n"
		s += "Press any key to continue..."
	case Accepting:
		if m.command_return_text == "" {
			s += "What would you like to do?\n"
		} else {
			s += m.command_return_text
		}
		s += m.text_input.View()
	}

	return s
}