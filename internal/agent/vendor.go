package agent

import (
	"context"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// onContent, if non-nil, is called with each piece of assistant text as it
// streams in, before the full response has finished generating.
type LLMVendor interface {
	getResponse(messages []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolParam, onContent func(delta string)) (openai.ChatCompletionMessage, error)
}

type OpenAIVendor struct {
	client *openai.Client
	model  string
}

func NewOpenAIVendor() *OpenAIVendor {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, reading env from environment")
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY is not set")
	}

	client := openai.NewClient(option.WithAPIKey(apiKey))

	return &OpenAIVendor{client: &client, model: "gpt-5.5"}
}

// getResponse streams the completion so onContent fires as tokens are
// generated instead of only after the full reply is done, then accumulates
// the stream into a single message for history bookkeeping.
func (o *OpenAIVendor) getResponse(messages []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolParam, onContent func(delta string)) (openai.ChatCompletionMessage, error) {
	stream := o.client.Chat.Completions.NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Model:    o.model,
		Messages: messages,
		Tools:    tools,
	})
	defer stream.Close()

	var acc openai.ChatCompletionAccumulator
	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)

		if delta := chunk.Choices; len(delta) > 0 && delta[0].Delta.Content != "" && onContent != nil {
			onContent(delta[0].Delta.Content)
		}
	}

	if err := stream.Err(); err != nil {
		return openai.ChatCompletionMessage{}, err
	}

	return acc.Choices[0].Message, nil
}
