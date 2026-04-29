package translate_test

import (
	"fmt"

	"github.com/Daviey/bulwarkai/internal/translate"
)

func ExampleExtractAnthropicPrompt() {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Hello"},
		},
	}
	fmt.Println(translate.ExtractAnthropicPrompt(body))

	// Output: Hello
}

func ExampleExtractOpenAIPrompt() {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "Be helpful"},
			map[string]interface{}{"role": "user", "content": "What is 2+2?"},
		},
	}
	prompt := translate.ExtractOpenAIPrompt(body)
	fmt.Println(prompt == "Be helpful What is 2+2?")

	// Output: true
}

func ExampleStrVal() {
	fmt.Println(translate.StrVal("hello", "default"))
	fmt.Println(translate.StrVal(42, "default"))
	fmt.Println(translate.StrVal(nil, "default"))

	// Output:
	// hello
	// default
	// default
}

func ExampleIntVal() {
	m := map[string]interface{}{"count": float64(42)}
	fmt.Println(translate.IntVal(m["count"]))

	// Output: 42
}
