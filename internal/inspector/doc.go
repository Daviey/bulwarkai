/*
Package inspector defines the screening interface and provides built-in
backends for content inspection.

The Inspector interface has two methods: InspectPrompt and InspectResponse.
Each returns a BlockResult when content should be blocked, or nil when it
passes. A Chain runs multiple inspectors in order, stopping at the first
block.

Built-in inspectors:

  - regexInspector: pattern-matches SSNs, credit cards, private keys, AWS
    keys, API keys, and credential patterns. Zero latency, no network.
  - modelArmorInspector: calls Google Model Armor's standalone API.
    Only loaded in non-strict modes. Fail-open on errors.
  - dlpInspector: calls Google Cloud DLP content:inspect.
    Opt-in via DLP_API=true. Fail-open on errors.

To add a new inspector, implement the Inspector interface and append it to
the chain in main.go.

Fail-open semantics: if a remote inspector is unreachable, it returns nil
(pass) rather than blocking all traffic.

EICAR-style test strings are provided as exported constants (TestSSN,
TestCreditCard, TestPrivateKey, TestAWSKey, TestAPIKey, TestCredentials).
These trigger the regex inspector with obviously fake data prefixed with
"BULWARKAI-TEST". The /test-strings endpoint returns all of them as JSON.
*/
package inspector
