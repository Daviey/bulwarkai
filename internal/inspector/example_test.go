package inspector_test

import (
	"context"
	"fmt"

	"github.com/Daviey/bulwarkai/internal/inspector"
)

func ExampleNewRegexInspector() {
	r := inspector.NewRegexInspector()
	fmt.Println(r.Name())

	br := r.InspectPrompt(context.Background(), "My SSN is 123-45-6789", "")
	if br != nil {
		fmt.Println(br.Reason)
	}

	br = r.InspectPrompt(context.Background(), "What is 2+2?", "")
	fmt.Println(br == nil)

	br = r.InspectResponse(context.Background(), "Your SSN is 123-45-6789", "")
	if br != nil {
		fmt.Println(br.Reason)
	}

	// Output:
	// regex
	// SSN detected
	// true
	// SSN in response
}

func ExampleChain() {
	chain := inspector.NewChain(inspector.NewRegexInspector())

	br := chain.ScreenPrompt(context.Background(), "card 1234567890123456", "")
	if br != nil {
		fmt.Println(br.Reason)
	}

	br = chain.ScreenPrompt(context.Background(), "Hello", "")
	fmt.Println(br == nil)

	// Output:
	// Credit card number detected
	// true
}

func ExampleChain_testMethods() {
	chain := inspector.NewChain(inspector.NewRegexInspector())
	methods := chain.TestMethods()
	fmt.Println(methods["regex"])

	br := chain.ScreenPrompt(context.Background(), methods["regex"], "")
	if br != nil {
		fmt.Println(br.Reason)
	}

	// Output:
	// BULWARKAI-TEST-SSN-000-00-0000
	// SSN detected
}
