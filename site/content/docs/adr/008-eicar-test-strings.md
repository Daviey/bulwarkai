+++
title = "ADR 008: EICAR-style test strings"
weight = 8
+++


Date: 2026-04-30

## Context

Testing whether the screening proxy works requires sending content that triggers inspectors. Using real sensitive data (actual SSNs, real API keys) in tests is a security problem: it shows up in logs, version control, and terminal history.

## Decision

Add hardcoded test strings as exported constants in the `inspector` package. Each string matches an existing regex pattern but is obviously fake, prefixed with `BULWARKAI-TEST`. Provide a `/test-strings` endpoint that returns all of them as JSON.

This follows the same principle as the [EICAR test file](https://en.wikipedia.org/wiki/EICAR_test_file) used to verify antivirus software: a safe, canonical string that proves the detection pipeline works end to end.

## Consequences

- Test strings are publicly visible in source code and could be used to probe whether a service is running Bulwarkai. This is acceptable because the strings only trigger blocking, not bypasses.
- Test strings are logged when blocked (same as any other block event). They are not treated differently in the audit trail.
- The `/test-strings` endpoint requires no authentication so that operators can verify the service without credentials.
- Adding new test strings for future inspectors (e.g. DLP-specific patterns) is a simple constant addition.
