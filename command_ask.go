package main

import (
	"context"
	"strings"

	"github.com/ansht2000/jones/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

func commandAsk(model *Model, args ...string) tea.Cmd {
	question := strings.Join(args, " ")
	if question == "" {
		model.addEntry(entryError, "Ask command expects a question")
		return nil
	}
	if model.active_repo == "" {
		model.addEntry(entryError, "Pick a repo to ask about with use <repo_name> first.")
		return nil
	}
	if model.client == nil {
		model.addEntry(entryError, "Can't answer questions without an API key: "+model.client_err.Error())
		return nil
	}

	client := model.client
	repo_analysis := model.analyses[model.active_repo]
	repo_path := model.repo_list[model.active_repo]
	return model.startTask("Thinking", func(ctx context.Context, send func(tea.Msg)) taskDoneMsg {
		options := agent.DefaultOptions()
		options.OnEvent = func(event agent.Event) {
			send(agentEventMsg{event: event})
		}
		answer, err := agent.Ask(ctx, client, repo_analysis, repo_path, question, options)
		if err != nil {
			return failed(err)
		}

		return taskDoneMsg{
			entries: []entry{verificationEntry(answer)},
			apply: func(m *Model) {
				// the answer is normally already there from streaming
				if m.live == "" {
					m.live = answer.Text
				}
			},
		}
	})
}

// Whether the answer checked out against the code it cites
func verificationEntry(answer *agent.Answer) entry {
	if !answer.Verified {
		return entry{kind: entryWarning, text: "⚠ This answer couldn't be verified:\n" + bullets(answer.Issues)}
	}
	if len(answer.Citations) == 0 {
		return entry{kind: entrySuccess, text: "✓ Verified"}
	}
	sources := make([]string, len(answer.Citations))
	for i, citation := range answer.Citations {
		sources[i] = citation.String()
	}
	return entry{kind: entrySuccess, text: "✓ Verified against " + strings.Join(sources, ", ")}
}
