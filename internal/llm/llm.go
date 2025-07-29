package llm

import "time"

func MockLLMCall() string {
	time.Sleep(time.Second)
	return "called"
}
