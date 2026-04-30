package policy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewEngine_Disabled(t *testing.T) {
	eng, err := NewEngine(context.Background(), false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if eng == nil {
		t.Fatal("engine should not be nil when disabled")
	}
	dec := eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "gemini-2.5-flash"})
	if !dec.Allowed {
		t.Fatal("disabled engine should allow everything")
	}
}

func TestNewEngine_DefaultPolicy(t *testing.T) {
	eng, err := NewEngine(context.Background(), true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	dec := eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "gemini-2.5-flash"})
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

	dec := eng.Evaluate(context.Background(), Input{Email: "admin@test.com", Model: "gemini-2.5-flash"})
	if !dec.Allowed {
		t.Fatal("admin should be allowed")
	}

	dec = eng.Evaluate(context.Background(), Input{Email: "evil@test.com", Model: "gemini-2.5-flash"})
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

	dec := eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "gemini-2.5-flash"})
	if !dec.Allowed {
		t.Fatal("allowed model should pass")
	}

	dec = eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "gemini-2.5-pro"})
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

	dec := eng.Evaluate(context.Background(), Input{Email: "fast@test.com", Model: "gemini-2.5-flash", Stream: true})
	if !dec.Allowed {
		t.Fatal("streamer should be allowed to stream")
	}

	dec = eng.Evaluate(context.Background(), Input{Email: "slow@test.com", Model: "gemini-2.5-flash", Stream: true})
	if dec.Allowed {
		t.Fatal("non-streamer should be denied streaming")
	}
	if dec.Reason != "streaming not allowed for your group" {
		t.Fatalf("got reason %q", dec.Reason)
	}

	dec = eng.Evaluate(context.Background(), Input{Email: "slow@test.com", Model: "gemini-2.5-flash", Stream: false})
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
	dec := eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "gemini-2.5-flash"})
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
	defer eng.Stop()
	dec := eng.Evaluate(context.Background(), Input{Email: "file@test.com", Model: "gemini-2.5-flash"})
	if !dec.Allowed {
		t.Fatal("should be allowed from file policy")
	}
	dec = eng.Evaluate(context.Background(), Input{Email: "other@test.com", Model: "gemini-2.5-flash"})
	if dec.Allowed {
		t.Fatal("should be denied by file policy")
	}
}

func TestNewEngine_PolicyFromURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, `package bulwarkai
default allow := false
allow if { input.email == "url@test.com" }
`)
	}))
	defer ts.Close()

	eng, err := NewEngineWithHTTP(context.Background(), true, "", ts.URL+"/policy.rego", ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Stop()

	dec := eng.Evaluate(context.Background(), Input{Email: "url@test.com", Model: "gemini-2.5-flash"})
	if !dec.Allowed {
		t.Fatal("should be allowed from url policy")
	}
	dec = eng.Evaluate(context.Background(), Input{Email: "other@test.com", Model: "gemini-2.5-flash"})
	if dec.Allowed {
		t.Fatal("should be denied by url policy")
	}
}

func TestNewEngine_PolicyFromURLError(t *testing.T) {
	_, err := NewEngineWithHTTP(context.Background(), true, "", "http://127.0.0.1:1/nonexistent", nil)
	if err == nil {
		t.Fatal("expected error from bad url")
	}
}

func TestNewEngine_PolicyURLNonHTTP(t *testing.T) {
	eng, err := NewEngine(context.Background(), true, "", `package bulwarkai
default allow := false
allow if { input.email == "inline@test.com" }
`)
	if err != nil {
		t.Fatal(err)
	}
	dec := eng.Evaluate(context.Background(), Input{Email: "inline@test.com", Model: "gemini-2.5-flash"})
	if !dec.Allowed {
		t.Fatal("inline policy should allow listed email")
	}
	dec = eng.Evaluate(context.Background(), Input{Email: "other@test.com", Model: "gemini-2.5-flash"})
	if dec.Allowed {
		t.Fatal("inline policy should deny unlisted email")
	}
}

func TestHotReload_FileChange(t *testing.T) {
	f, err := os.CreateTemp("", "hotreload-*.rego")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	fmt.Fprint(f, `package bulwarkai
default allow := false
allow if { input.email == "first@test.com" }
`)
	f.Close()

	eng, err := NewEngine(context.Background(), true, f.Name(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Stop()

	dec := eng.Evaluate(context.Background(), Input{Email: "first@test.com", Model: "m"})
	if !dec.Allowed {
		t.Fatal("initial policy should allow first@test.com")
	}
	dec = eng.Evaluate(context.Background(), Input{Email: "second@test.com", Model: "m"})
	if dec.Allowed {
		t.Fatal("initial policy should deny second@test.com")
	}

	os.WriteFile(f.Name(), []byte(`package bulwarkai
default allow := false
allow if { input.email == "second@test.com" }
`), 0644)

	time.Sleep(11500 * time.Millisecond)

	dec = eng.Evaluate(context.Background(), Input{Email: "second@test.com", Model: "m"})
	if !dec.Allowed {
		t.Fatal("after reload, second@test.com should be allowed")
	}
	dec = eng.Evaluate(context.Background(), Input{Email: "first@test.com", Model: "m"})
	if dec.Allowed {
		t.Fatal("after reload, first@test.com should be denied")
	}
}

func TestHotReload_BadFileContent(t *testing.T) {
	f, err := os.CreateTemp("", "badreload-*.rego")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	fmt.Fprint(f, `package bulwarkai
default allow := true
`)
	f.Close()

	eng, err := NewEngine(context.Background(), true, f.Name(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Stop()

	os.WriteFile(f.Name(), []byte("!!! bad rego !!!"), 0644)
	time.Sleep(11500 * time.Millisecond)

	dec := eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "m"})
	if !dec.Allowed {
		t.Fatal("bad reload should keep old policy")
	}
}

func TestHotReload_URLChange(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			fmt.Fprint(w, `package bulwarkai
default allow := false
allow if { input.email == "v1@test.com" }
`)
		} else {
			fmt.Fprint(w, `package bulwarkai
default allow := false
allow if { input.email == "v2@test.com" }
`)
		}
	}))
	defer ts.Close()

	eng, err := newEngine(context.Background(), true, "", ts.URL+"/policy.rego", ts.Client(), 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Stop()

	dec := eng.Evaluate(context.Background(), Input{Email: "v1@test.com", Model: "m"})
	if !dec.Allowed {
		t.Fatal("initial url policy should allow v1@test.com")
	}

	time.Sleep(2500 * time.Millisecond)

	dec = eng.Evaluate(context.Background(), Input{Email: "v2@test.com", Model: "m"})
	if !dec.Allowed {
		t.Fatal("after url reload, v2@test.com should be allowed")
	}
}

func TestHotReload_URLBadResponse(t *testing.T) {
	first := true
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if first {
			first = false
			fmt.Fprint(w, `package bulwarkai
default allow := true
`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	eng, err := newEngine(context.Background(), true, "", ts.URL+"/policy.rego", ts.Client(), 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Stop()

	time.Sleep(2500 * time.Millisecond)

	dec := eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "m"})
	if !dec.Allowed {
		t.Fatal("bad url reload should keep old policy")
	}
}

func TestStop(t *testing.T) {
	f, err := os.CreateTemp("", "stop-*.rego")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	fmt.Fprint(f, `package bulwarkai
default allow := true
`)
	f.Close()

	eng, err := NewEngine(context.Background(), true, f.Name(), "")
	if err != nil {
		t.Fatal(err)
	}
	eng.Stop()

	os.WriteFile(f.Name(), []byte(`package bulwarkai
default allow := false
`), 0644)

	time.Sleep(1500 * time.Millisecond)

	dec := eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "m"})
	if !dec.Allowed {
		t.Fatal("after stop, watcher should not reload")
	}
}

func TestIsHTTPURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://storage.googleapis.com/bucket/policy.rego", true},
		{"http://localhost/policy.rego", true},
		{"gs://bucket/policy.rego", false},
		{"/tmp/policy.rego", false},
		{"package bulwarkai", false},
	}
	for _, tt := range tests {
		if got := isHTTPURL(tt.input); got != tt.want {
			t.Errorf("isHTTPURL(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestNewEngine_LargeURLResponse(t *testing.T) {
	bigPolicy := "package bulwarkai\ndefault allow := true\n" + strings.Repeat("# comment line\n", 10000)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, bigPolicy)
	}))
	defer ts.Close()

	eng, err := NewEngineWithHTTP(context.Background(), true, "", ts.URL+"/policy.rego", ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	eng.Stop()
}
