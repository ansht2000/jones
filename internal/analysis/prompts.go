package analysis

import (
	"bytes"
	"fmt"
	"path"
	"strings"
)

func filePrompt(rel_path string, content []byte, size int64) string {
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "Path: %s\n", rel_path)
	if int64(len(content)) < size {
		fmt.Fprintf(&prompt, "Only the first %d of the file's %d bytes are shown.\n", len(content), size)
	}
	prompt.WriteString("\n<contents>\n")
	// cutting a file short can split a multi byte character
	prompt.Write(bytes.ToValidUTF8(content, []byte("�")))
	prompt.WriteString("\n</contents>")
	return prompt.String()
}

func dirPrompt(dir *dirEntry, result *Analysis) string {
	return fmt.Sprintf("Directory: %s\n\nContents:\n%s", dir.rel_path, contentsList(dir, result))
}

// contents is the contentsList of the repo's root
func overviewPrompt(repo_name string, contents string, readme *fileEntry, readme_content []byte) string {
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "Repository: %s\n\nTop level contents:\n%s", repo_name, contents)
	if len(readme_content) > 0 {
		fmt.Fprintf(&prompt, "\nREADME (%s):\n", readme.rel_path)
		if int64(len(readme_content)) < readme.size {
			fmt.Fprintf(&prompt, "Only the first %d of its %d bytes are shown.\n", len(readme_content), readme.size)
		}
		prompt.WriteString("<readme>\n")
		prompt.Write(bytes.ToValidUTF8(readme_content, []byte("�")))
		prompt.WriteString("\n</readme>")
	}
	return prompt.String()
}

// one line per item in a directory with its summary, subdirectories first
func contentsList(dir *dirEntry, result *Analysis) string {
	if len(dir.dirs) == 0 && len(dir.files) == 0 {
		return "(empty)\n"
	}

	var list strings.Builder
	for _, child := range dir.dirs {
		fmt.Fprintf(&list, "- %s/ (directory): %s\n", path.Base(child.rel_path), oneLine(result.Dirs[child.rel_path].Summary))
	}
	for _, file := range dir.files {
		summary := result.Files[file.rel_path].Summary
		fmt.Fprintf(&list, "- %s: %s", path.Base(file.rel_path), oneLine(summary.Purpose))
		if len(summary.KeySymbols) > 0 {
			fmt.Fprintf(&list, " Defines %s.", strings.Join(summary.KeySymbols, ", "))
		}
		list.WriteString("\n")
	}
	return list.String()
}

// collapse whitespace, so a multi line summary fits on one line of a list
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
