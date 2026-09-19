package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/ansht2000/jones/internal/analysis"
	"github.com/ansht2000/jones/internal/llm"
	"github.com/ansht2000/jones/internal/repo"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"
)

type State int

const (
	Initial State = iota
	Accepting
	Running
)

const logo = `
     ____
    |    | ____   ____   ____   ______
    |    |/  _ \ /    \_/ __ \ /  ___/
/\__|    (  |_| )   |  \  ___/ \___ \
\________|\____/|___|  /\_____ ____  \
                     \/             \/
`

type Model struct {
	state      State
	commands   CommandMap
	text_input textinput.Model
	viewport   viewport.Model
	spinner    spinner.Model
	progress   progress.Model
	width      int
	height     int
	// everything shown above the input, and the same entries rendered at the current width
	transcript          []entry
	rendered_transcript string
	// the answer being streamed, shown below the transcript until it's done
	live string
	// the background job running, nil when there isn't one
	task         *task
	repo_manager *repo.RepoManager
	repo_list    map[string]string
	// the repo questions are asked about
	active_repo string
	// analyses loaded or made this session, by repo name
	analyses map[string]*analysis.Analysis
	// nil when there's no API key, with client_err saying why
	client     llm.Client
	client_err error
	model_name string
}

func initialModel(repo_manager *repo.RepoManager, client llm.Client, model_name string, client_err error) Model {
	ti := textinput.New()
	ti.Placeholder = "type a command, or help"
	ti.Focus()

	m := Model{
		state:        Initial,
		commands:     getCommands(),
		text_input:   ti,
		viewport:     viewport.New(0, 0),
		spinner:      spinner.New(spinner.WithSpinner(spinner.Dot)),
		progress:     progress.New(progress.WithDefaultGradient()),
		repo_manager: repo_manager,
		repo_list:    getClonedRepos(repo_manager.Root),
		analyses:     map[string]*analysis.Analysis{},
		client:       client,
		client_err:   client_err,
		model_name:   model_name,
	}
	// a guess until the terminal reports its size
	m.resize(80, 24)
	m.addEntry(entryNote, "Type help to see what jones can do.")
	if client_err != nil {
		m.addEntry(entryWarning, "Analyzing repos and asking questions are turned off: "+client_err.Error())
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		// ctrl+c always quits, stopping anything that's running first
		if msg.Type == tea.KeyCtrlC {
			m.cancelTask()
			return m, tea.Quit
		}
		if m.state == Initial {
			m.state = Accepting
			return m, nil
		}
		return m.handleKey(msg)

	case CommandMsg:
		return m.runCommand(msg)

	case spinner.TickMsg:
		// the spinner only turns while a task runs
		if m.task == nil {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progressMsg:
		if m.task != nil {
			m.task.status = capitalize(string(msg.progress.Stage))
			m.task.progress = &msg.progress
			m.task.notice = ""
		}
		return m, m.waitForTask()

	case agentEventMsg:
		m.handleAgentEvent(msg.event)
		return m, m.waitForTask()

	case retryMsg:
		// sent from outside the task, so it doesn't wait for the task's next message
		if m.task != nil {
			m.task.notice = fmt.Sprintf("an API call failed, retrying in %s", msg.delay.Round(time.Second))
		}
		return m, nil

	case taskDoneMsg:
		m.finishTask(msg)
		return m, nil
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		input := strings.TrimSpace(m.text_input.Value())
		if input == "" {
			return m, nil
		}
		m.text_input.SetValue("")
		m.addEntry(entryCommand, input)
		m.viewport.GotoBottom()
		command_name, command_args := parseCommand(input)
		return m, sendCommandMsg(command_name, command_args)
	case tea.KeyEsc:
		if m.task != nil {
			m.task.status = "Cancelling"
			m.cancelTask()
		} else {
			m.text_input.SetValue("")
		}
	case tea.KeyUp:
		m.viewport.ScrollUp(1)
	case tea.KeyDown:
		m.viewport.ScrollDown(1)
	case tea.KeyPgUp:
		m.viewport.PageUp()
	case tea.KeyPgDown:
		m.viewport.PageDown()
	default:
		var cmd tea.Cmd
		m.text_input, cmd = m.text_input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) runCommand(msg CommandMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if command, ok := m.commands[msg.command_name]; ok {
		cmd = command.callback(&m, msg.command_args...)
	} else if m.active_repo != "" {
		// anything that isn't a command is a question about the repo in use
		cmd = commandAsk(&m, append([]string{msg.command_name}, msg.command_args...)...)
	} else {
		m.addEntry(entryError, fmt.Sprintf("Unknown command %q. Type help to see the commands, or pick a repo with use to ask questions about it.", msg.command_name))
	}
	return m, cmd
}

func (m *Model) resize(width, height int) {
	// checked before the size changes, which moves where the bottom is
	at_bottom := m.viewport.AtBottom()

	m.width, m.height = width, height
	// the task line, the input, and the status bar below the transcript take a line each
	m.viewport.Width = width
	m.viewport.Height = max(height-3, 1)
	m.text_input.Width = max(width-lipgloss.Width(m.text_input.Prompt)-1, 1)
	m.progress.Width = min(40, max(width/3, 10))
	m.rerender()
	if at_bottom {
		m.viewport.GotoBottom()
	}
}

func (m Model) View() string {
	if m.state == Initial {
		return logo + "\n\nPress any key to continue..."
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.viewport.View(),
		m.taskLine(),
		m.text_input.View(),
		m.statusBar(),
	)
}

// What the running task is doing, empty when nothing is running
func (m Model) taskLine() string {
	if m.task == nil {
		return ""
	}
	line := m.spinner.View() + " " + m.task.status
	if progress := m.task.progress; progress != nil && progress.Total > 0 {
		line += fmt.Sprintf(" %d/%d ", progress.Done, progress.Total) + m.progress.ViewAs(float64(progress.Done)/float64(progress.Total))
	}
	if m.task.notice != "" {
		line += warning_style.Render(" (" + m.task.notice + ")")
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(line)
}

func (m Model) statusBar() string {
	repo_status := "no repo in use"
	if m.active_repo != "" {
		repo_status = "using " + m.active_repo
	}
	model_status := m.model_name
	if m.client == nil {
		model_status = "no API key"
	}
	left := strings.Join([]string{"jones", repo_status, model_status}, " · ")

	hints := "ctrl+c quit"
	if m.task != nil {
		hints = "esc cancel · " + hints
	}
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(hints)-2, 1)
	return status_style.MaxWidth(m.width).Render(" " + left + strings.Repeat(" ", gap) + hints + " ")
}
