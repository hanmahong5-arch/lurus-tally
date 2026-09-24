package ai

// BuildMessagesForTest exposes the package-private buildMessages helper to
// _test files in this directory. Test-only — do not use from production code.
func BuildMessagesForTest(in ChatInput) []messageForTest {
	msgs := buildMessages(in)
	out := make([]messageForTest, 0, len(msgs))
	for _, m := range msgs {
		var content string
		switch v := m.Content.(type) {
		case string:
			content = v
		}
		out = append(out, messageForTest{Role: m.Role, Content: content})
	}
	return out
}

// messageForTest mirrors llmclient.Message but with a string-only Content
// so test assertions stay simple.
type messageForTest struct {
	Role    string
	Content string
}
