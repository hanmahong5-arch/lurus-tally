package llm

import "regexp"

// Redact and RedactJSON are best-effort safeguards that reduce the likelihood
// of PII appearing in trace attributes. They are NOT a compliance control —
// they may miss novel patterns and should not be treated as a guarantee. The
// redaction runs on the already-truncated attribute value, so patterns split by
// the 4096-byte cut may slip through.
//
// Regexes are compiled once at package init to avoid repeated allocation on
// the (infrequent) LLM hot path.

var (
	// rEmail matches user@host.tld forms. Deliberately narrow: requires exactly
	// one @ and at least one dot in the domain.
	rEmail = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

	// rPhoneCN matches mainland China mobile numbers: an optional "+86" or "86"
	// country-code prefix followed by 1[3-9]\d{9}. The 11-digit number is in
	// capture group 1; surrounding non-digit context is preserved by
	// ReplaceAllStringFunc. Prevents matching arbitrary numeric IDs whose first
	// digit is not in [3-9] after the leading "1".
	rPhoneCN = regexp.MustCompile(`(\+?86)?(1[3-9]\d{9})`)

	// rIDCN matches 18-character Chinese national identity cards: 17 digits
	// followed by a digit or 'X'/'x'.
	rIDCN = regexp.MustCompile(`\b\d{17}[\dXx]\b`)

	// rCard matches 13-19 consecutive digits as a crude credit/debit card
	// heuristic. This will produce false positives (e.g. large numeric IDs),
	// which is acceptable for a best-effort trace sanitiser.
	rCard = regexp.MustCompile(`\b\d{13,19}\b`)

	// rToken matches strings of length ≥ 32 that contain at least one uppercase
	// letter, one lowercase letter, and one digit — a heuristic for base64/hex
	// API keys and bearer tokens. Anchored to word boundaries to reduce false
	// positives on prose text.
	rToken = regexp.MustCompile(`\b[A-Za-z0-9+/=_\-]{32,}\b`)
	// rTokenRequired confirms the matched candidate contains all three character
	// classes before replacement.
	rTokenRequired = regexp.MustCompile(`[A-Z].*[a-z].*[0-9]|[A-Z].*[0-9].*[a-z]|[a-z].*[A-Z].*[0-9]|[a-z].*[0-9].*[A-Z]|[0-9].*[A-Z].*[a-z]|[0-9].*[a-z].*[A-Z]`)

	// rJSONSecret matches JSON key-value pairs for common secret field names.
	// Keys are matched case-insensitively; only the value (between quotes) is
	// replaced while the key and surrounding structure are preserved.
	rJSONSecret = regexp.MustCompile(`(?i)("(?:password|secret|api_key|apikey|access_token|auth_token|token|private_key)"\s*:\s*)"([^"]*)"`)
)

// Redact replaces recognisable PII patterns in s with labelled placeholders.
// The function is pure and safe for concurrent use.
func Redact(s string) string {
	// Order matters: apply more-specific patterns first so their replacements
	// (e.g. <email>) are not re-matched by less-specific ones.

	// 1. Emails before phone numbers to avoid ambiguity.
	s = rEmail.ReplaceAllString(s, "<email>")

	// 2. CN identity card before phone numbers: the 18-digit ID contains a
	//    substring that satisfies the phone pattern (e.g. digits 7-17 in a CN
	//    ID often start with 1[3-9]). Replacing ID first prevents false phone
	//    matches inside ID strings.
	s = rIDCN.ReplaceAllString(s, "<id-cn>")

	// 3. CN mobile numbers — replace the 11-digit number (and optional country-
	//    code prefix) with the placeholder.
	s = rPhoneCN.ReplaceAllString(s, "<phone-cn>")

	// 4. Long token-like strings before card numbers so they are caught first.
	s = rToken.ReplaceAllStringFunc(s, func(candidate string) string {
		if len(candidate) >= 32 && rTokenRequired.MatchString(candidate) {
			return "<token>"
		}
		return candidate
	})

	// 5. Credit/debit card heuristic (13-19 consecutive digits). Runs last to
	//    avoid matching the digit tails of already-replaced placeholders.
	s = rCard.ReplaceAllString(s, "<card>")

	return s
}

// RedactJSON replaces the values of well-known secret fields in JSON-like
// strings. The key matching is case-insensitive; the surrounding JSON
// structure is preserved. Non-JSON input is returned unchanged.
func RedactJSON(s string) string {
	return rJSONSecret.ReplaceAllString(s, `${1}"<redacted>"`)
}
