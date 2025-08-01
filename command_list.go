package main

import "fmt"

func commandList(model *Model, args ...string) string {
	return_str := "Available repos:\n"
	for repo := range model.repo_list {
		return_str += fmt.Sprintf(repo + "\n")
	}
	return return_str
}
