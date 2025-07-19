package main

import (
	"fmt"

	"github.com/ansht2000/jones/internal/git"
)

func commandClone(model *Model, args ...string) string {
	if len(args) != 1 {
		return fmt.Sprintf("Clone command expects 1 argument, received %d\n", len(args))
	}
	err := git.CloneRepo(args[0])
	if err != nil {
		return err.Error()
	}
	return "Cloned repo successfully!\n"
}