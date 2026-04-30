package policy

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestNewEngine_Disabled(t *testing.T) {
	eng, err := NewEngine(context.Background(), false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if eng == nil {
		t.Fatal("engine should not be nil when disabled")
	}
	dec, err := eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("disabled engine should allow everything")
	}
}

func TestNewEngine_DefaultPolicy(t *testing.T) {
	eng, err := NewEngine(context.Background(), true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	dec, err := eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("default policy should allow")
	}
}

func TestNewEngine_CustomPolicy(t *testing.T) {
	policy := `
package bulwarkai

default allow := false

allow if {
	input.email == "admin@test.com"
}
`
	eng, err := NewEngine(context.Background(), true, "", policy)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := eng.Evaluate(context.Background(), Input{Email: "admin@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("admin should be allowed")
	}

	dec, err = eng.Evaluate(context.Background(), Input{Email: "evil@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed {
		t.Fatal("unknown user should be denied")
	}
}

func TestNewEngine_CustomPolicyWithReason(t *testing.T) {
	policy := `
package bulwarkai

default allow := false

deny_reason := "model not permitted" if {
	not model_allowed[input.model]
}

allow if {
	model_allowed[input.model]
}

model_allowed contains "gemini-2.5-flash"
`
	eng, err := NewEngine(context.Background(), true, "", policy)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("allowed model should pass")
	}

	dec, err = eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "gemini-2.5-pro"})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed {
		t.Fatal("disallowed model should be denied")
	}
	if dec.Reason != "model not permitted" {
		t.Fatalf("got reason %q", dec.Reason)
	}
}

func TestNewEngine_StreamEnforcement(t *testing.T) {
	policy := `
package bulwarkai

default allow := false

deny_reason := "streaming not allowed for your group" if {
	input.stream
	not streamers[input.email]
}

allow if {
	not input.stream
}

allow if {
	input.stream
	streamers[input.email]
}

streamers contains "fast@test.com"
`
	eng, err := NewEngine(context.Background(), true, "", policy)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := eng.Evaluate(context.Background(), Input{Email: "fast@test.com", Model: "gemini-2.5-flash", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("streamer should be allowed to stream")
	}

	dec, err = eng.Evaluate(context.Background(), Input{Email: "slow@test.com", Model: "gemini-2.5-flash", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed {
		t.Fatal("non-streamer should be denied streaming")
	}
	if dec.Reason != "streaming not allowed for your group" {
		t.Fatalf("got reason %q", dec.Reason)
	}

	dec, err = eng.Evaluate(context.Background(), Input{Email: "slow@test.com", Model: "gemini-2.5-flash", Stream: false})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("non-streaming should always be allowed")
	}
}

func TestNewEngine_EngineClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	eng, err := NewEngine(ctx, true, "", "")
	if err != nil {
		return
	}
	dec, err := eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("should be allowed with default policy")
	}
}

func TestNewEngine_BadPolicy(t *testing.T) {
	_, err := NewEngine(context.Background(), true, "", "this is not valid rego !!!")
	if err == nil {
		t.Fatal("bad policy should error")
	}
}

func TestNewEngine_PolicyFromFile(t *testing.T) {
	f, err := os.CreateTemp("", "policy-*.rego")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	fmt.Fprint(f, `package bulwarkai
default allow := false
allow if { input.email == "file@test.com" }
`)
	f.Close()

	eng, err := NewEngine(context.Background(), true, f.Name(), "")
	if err != nil {
		t.Fatal(err)
	}
	dec, err := eng.Evaluate(context.Background(), Input{Email: "file@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("should be allowed from file policy")
	}
	dec, err = eng.Evaluate(context.Background(), Input{Email: "other@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed {
		t.Fatal("should be denied by file policy")
	}
}
