package main

import "fmt"

func commandList(model *Model, args ...string) string {
	var return_str string
	if len(model.repo_list) > 0 {
		return_str = "Available repos:\n\n"
		for repo := range model.repo_list {
			return_str += fmt.Sprintf(repo + "\n")
		}
	} else {
		return_str = "No repos found, clone some!"
	}
	return return_str + "\n"
}
