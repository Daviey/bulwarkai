package streaming_test

import (
	"fmt"

	"github.com/Daviey/bulwarkai/internal/streaming"
)

func ExampleParseGeminiChunk() {
	line := `{"candidates":[{"content":{"parts":[{"text":"Hello"}]},"finishReason":"STOP"}]}`
	text, finish, _ := streaming.ParseGeminiChunk(line)
	fmt.Println(text)
	fmt.Println(finish)

	// Output:
	// Hello
	// STOP
}
