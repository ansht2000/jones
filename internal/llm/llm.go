package llm

import (
	"context"
	"time"

	"google.golang.org/genai"
)

func MockLLMCall() string {
	time.Sleep(time.Second)
	return "called"
}

func NewClientWithContext(ctx context.Context) (*genai.Client, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{})
	if err != nil {
		return &genai.Client{}, err
	}

	return client, nil
}

func GenerateFileSummary(ctx context.Context, client *genai.Client) (string, error) {
	res, err := client.Models.GenerateContent(
		ctx,
		"gemini-2.5-flash",
		genai.Text("Say hello"),
		&genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromBytes([]byte(SYSTEM_PROMPT), "text/plain", ""),
		},
	)
	if err != nil {
		return "", err
	}

	return res.Text(), nil
}