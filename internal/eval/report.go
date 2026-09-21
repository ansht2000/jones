package eval

import (
	"fmt"
	"strings"
	"time"
)

// Totals for one system across every question
type Summary struct {
	System  System
	Answers int
	Errors  int
	// Answers that mention every group of required phrases
	Complete int
	// Answers the judge looked at, and the ones it found correct
	Judged  int
	Correct int
	// Citations, the ones that don't point at real lines, and the answers with any of those
	Citations           int
	BadCitations        int
	AnswersWithBadCites int
	// Answers the agent verified, only for Verified
	Verified int
	Latency  time.Duration
}

func (r *Result) Summaries(systems []System) []Summary {
	summaries := make([]Summary, len(systems))
	for i, system := range systems {
		summary := Summary{System: system}
		for _, answer := range r.Answers {
			if answer.System != system {
				continue
			}
			summary.Answers++
			summary.Latency += answer.Latency
			if answer.Answer == "" && answer.Error != "" {
				summary.Errors++
				continue
			}
			if answer.Score.Complete() {
				summary.Complete++
			}
			if answer.Score.Correct != nil {
				summary.Judged++
				if *answer.Score.Correct {
					summary.Correct++
				}
			}
			summary.Citations += answer.Score.Citations
			summary.BadCitations += answer.Score.BadCitations
			if answer.Score.BadCitations > 0 {
				summary.AnswersWithBadCites++
			}
			if answer.Score.Verified {
				summary.Verified++
			}
		}
		summaries[i] = summary
	}
	return summaries
}

// The results as Markdown tables, header describes the run
func (r *Result) Markdown(systems []System, header string) string {
	var report strings.Builder
	report.WriteString("# jones evaluation\n\n")
	if header != "" {
		report.WriteString(header + "\n\n")
	}

	report.WriteString("## Analysis\n\n")
	report.WriteString("| Repo | Files | Directories | First analysis | Again, nothing changed | One call at a time |\n")
	report.WriteString("|---|---|---|---|---|---|\n")
	for _, repo := range r.Repos {
		sequential := "not measured"
		if repo.SequentialRun > 0 {
			sequential = formatDuration(repo.SequentialRun)
		}
		fmt.Fprintf(&report, "| %s | %d | %d | %s | %s | %s |\n", repo.Name, repo.Files, repo.Dirs,
			formatDuration(repo.FirstRun), formatDuration(repo.CachedRun), sequential)
	}

	summaries := r.Summaries(systems)
	questions := 0
	if len(summaries) > 0 {
		questions = summaries[0].Answers
	}
	fmt.Fprintf(&report, "\n## Answers (%d questions)\n\n", questions)
	report.WriteString("| System | Correct | Mentions the key facts | Citations | Bad citations | Answers with a bad citation | Verified | Average time | Errors |\n")
	report.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	for _, summary := range summaries {
		correct := "not judged"
		if summary.Judged > 0 {
			correct = fraction(summary.Correct, summary.Judged)
		}
		verified := "-"
		if summary.System == Verified {
			verified = fraction(summary.Verified, summary.Answers)
		}
		average := time.Duration(0)
		if summary.Answers > 0 {
			average = summary.Latency / time.Duration(summary.Answers)
		}
		fmt.Fprintf(&report, "| %s | %s | %s | %d | %d | %s | %s | %s | %d |\n", summary.System, correct,
			fraction(summary.Complete, summary.Answers), summary.Citations, summary.BadCitations,
			fraction(summary.AnswersWithBadCites, summary.Answers), verified, formatDuration(average), summary.Errors)
	}

	report.WriteString("\n## Each question\n\n")
	report.WriteString("✓ correct, ✗ wrong, ? not judged, ! failed, and the number of bad citations when there are any\n\n")
	report.WriteString("| Repo | Question |")
	for _, system := range systems {
		fmt.Fprintf(&report, " %s |", system)
	}
	report.WriteString("\n|---|---|" + strings.Repeat("---|", len(systems)) + "\n")

	type key struct{ repo, question string }
	marks := map[key]map[System]string{}
	order := []key{}
	for _, answer := range r.Answers {
		k := key{answer.Repo, answer.Question}
		if marks[k] == nil {
			marks[k] = map[System]string{}
			order = append(order, k)
		}
		marks[k][answer.System] = mark(answer)
	}
	for _, k := range order {
		fmt.Fprintf(&report, "| %s | %s |", k.repo, strings.ReplaceAll(k.question, "|", "\\|"))
		for _, system := range systems {
			fmt.Fprintf(&report, " %s |", marks[k][system])
		}
		report.WriteString("\n")
	}
	return report.String()
}

func mark(answer AnswerResult) string {
	if answer.Answer == "" && answer.Error != "" {
		return "!"
	}
	result := "?"
	if correct := answer.Score.Correct; correct != nil {
		result = "✗"
		if *correct {
			result = "✓"
		}
	}
	if answer.Score.BadCitations > 0 {
		result += fmt.Sprintf(" (%d bad)", answer.Score.BadCitations)
	}
	return result
}

func fraction(part, whole int) string {
	if whole == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/%d (%d%%)", part, whole, part*100/whole)
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(100 * time.Millisecond).String()
}
