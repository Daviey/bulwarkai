package inspector

import (
	"context"
	"regexp"
)

type inspectionRule struct {
	Pattern *regexp.Regexp
	Name    string
}

type regexInspector struct {
	promptRules   []inspectionRule
	responseRules []inspectionRule
}

const (
	TestSSN         = "BULWARKAI-TEST-SSN-000-00-0000"                //nosec G101
	TestCreditCard  = "BULWARKAI-TEST-CC-0000000000000000"            //nosec G101
	TestPrivateKey  = "BULWARKAI-TEST-KEY-BEGIN RSA PRIVATE KEY-END"  //nosec G101
	TestAWSKey      = "BULWARKAI-TEST-AWS-AKIA0000000000000000"       //nosec G101
	TestAPIKey      = "BULWARKAI-TEST-API-sk-00000000000000000000"    //nosec G101
	TestCredentials = "BULWARKAI-TEST-CRED-test@example.com password" //nosec G101
)

func (r *regexInspector) TestMethod() string { return TestSSN }

func NewRegexInspector() *regexInspector {
	return &regexInspector{
		promptRules: []inspectionRule{
			{regexp.MustCompile(`\b\d{3}[-.]?\d{2}[-.]?\d{4}\b`), "SSN detected"},
			{regexp.MustCompile(`\b\d{16}\b`), "Credit card number detected"},
			{regexp.MustCompile(`(?i)\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b.*\b(password|passwd|pwd)\b`), "Credentials in prompt"},
			{regexp.MustCompile(`(?i)\b(BEGIN\s+(RSA|DSA|EC|OPENSSH)\s+PRIVATE\s+KEY)`), "Private key detected"},
			{regexp.MustCompile(`\b(AKIA[0-9A-Z]{16})\b`), "AWS access key detected"},
			{regexp.MustCompile(`(sk-[a-zA-Z0-9]{20,})`), "API key detected"},
		},
		responseRules: []inspectionRule{
			{regexp.MustCompile(`\b\d{3}[-.]?\d{2}[-.]?\d{4}\b`), "SSN in response"},
			{regexp.MustCompile(`(?i)\b(BEGIN\s+(RSA|DSA|EC|OPENSSH)\s+PRIVATE\s+KEY)`), "Private key in response"},
		},
	}
}

func (r *regexInspector) Name() string { return "regex" }

func (r *regexInspector) InspectPrompt(_ context.Context, text string, _ string) *BlockResult {
	for _, rule := range r.promptRules {
		if rule.Pattern.MatchString(text) {
			return &BlockResult{Blocked: true, Reason: rule.Name}
		}
	}
	return nil
}

func (r *regexInspector) InspectResponse(_ context.Context, text string, _ string) *BlockResult {
	for _, rule := range r.responseRules {
		if rule.Pattern.MatchString(text) {
			return &BlockResult{Blocked: true, Reason: rule.Name}
		}
	}
	return nil
}
