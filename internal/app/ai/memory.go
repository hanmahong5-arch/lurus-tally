// Package ai — memory integration helpers for the AI Drawer.
//
// These functions implement the recall+write pattern:
//   - Before sending to LLM: search memorus for relevant past context
//   - After LLM responds: asynchronously write a summary back to memorus
//
// Degradation contract: if memorus is unavailable, both paths silently skip —
// the AI Drawer continues working without memory context.
package ai

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// asyncMemoryWriteTimeout bounds a fire-and-forget memory write. It is detached
// from the request context (which ends when the HTTP response is sent) but must
// still be bounded so a hung memorus connection cannot pin the goroutine and its
// connection indefinitely.
const asyncMemoryWriteTimeout = 5 * time.Second

// MemoryClient is the interface the orchestrator uses for memory operations.
// memorusclient.Client satisfies this interface; tests supply a mock.
type MemoryClient interface {
	Search(ctx context.Context, userID, query string, limit int) ([]memorusclient.Memory, error)
	Add(ctx context.Context, userID, content string, meta map[string]any) (*memorusclient.Memory, error)
}

const memorySearchLimit = 5

// AugmentMessagesWithMemory formats retrieved memories into a context prefix
// that is prepended to the user message before sending to the LLM.
//
// The returned string is the user message possibly prefixed with a
// "--- 历史记忆 ---" block containing relevant past context.
func AugmentMessagesWithMemory(memories []memorusclient.Memory, userMessage string) string {
	if len(memories) == 0 {
		return userMessage
	}
	var b strings.Builder
	b.WriteString("--- 历史记忆（仅供参考）---\n")
	for _, m := range memories {
		b.WriteString("• ")
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	b.WriteString(userMessage)
	return b.String()
}

// AugmentMessagesWithMemoryOrFallback searches memorus for memories relevant
// to userMessage and returns the augmented message. On any error (including
// ErrUnavailable) it returns the original userMessage unchanged.
func AugmentMessagesWithMemoryOrFallback(mc MemoryClient, ctx context.Context, userID, userMessage string) string {
	return AugmentWithCustomerMemory(mc, ctx, userID, userMessage, nil)
}

// FilteredSearcher is implemented by memory clients that can restrict a search
// to memories whose metadata matches (memorusclient.Client does).
type FilteredSearcher interface {
	SearchWithFilter(ctx context.Context, userID, query string, limit int, metaEq map[string]string) ([]memorusclient.Memory, error)
}

// customerMemoryLimit bounds the recall of one customer's own memories: a
// question about a customer ("接待张三要注意什么") wants all of them, and they
// rarely share words with the question.
const customerMemoryLimit = 10

// AugmentWithCustomerMemory is AugmentMessagesWithMemoryOrFallback for a
// message about customer (nil = not about exactly one customer).
//
// With a customer, the customer's own memories are fetched by filter as well
// as by similarity, and every memory about a DIFFERENT customer is dropped —
// "张三喜欢少糖" must never reach a prompt about 李四. The customer's own
// memories skip the relative score floor (they are on topic by construction);
// superseded values are still dropped.
func AugmentWithCustomerMemory(mc MemoryClient, ctx context.Context, userID, userMessage string, customer *CustomerRef) string {
	if mc == nil {
		return userMessage
	}
	memories, err := mc.Search(ctx, userID, userMessage, memorySearchLimit)
	if err != nil {
		return userMessage
	}
	if customer != nil {
		subject := CustomerSubject(customer.ID)
		var own []memorusclient.Memory
		if fs, ok := mc.(FilteredSearcher); ok {
			own, _ = fs.SearchWithFilter(ctx, userID, userMessage, customerMemoryLimit,
				map[string]string{MemorySubjectKey: subject})
		}
		memories = customerMemories(own, memories, subject)
	} else {
		memories = relevantMemories(memories)
	}
	if len(memories) == 0 {
		return userMessage
	}
	return AugmentMessagesWithMemory(memories, userMessage)
}

// customerMemories merges the customer's own memories (first) with the
// similarity hits, drops hits about other customers, and applies the usual
// relevance rules to the untagged remainder.
func customerMemories(own, similar []memorusclient.Memory, subject string) []memorusclient.Memory {
	seen := make(map[string]bool, len(own)+len(similar))
	var mine, untagged []memorusclient.Memory
	for _, m := range append(append([]memorusclient.Memory{}, own...), similar...) {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		switch memorySubject(m) {
		case subject:
			mine = append(mine, m)
		case "":
			untagged = append(untagged, m)
		}
	}
	out := relevantMemories(untagged)
	for _, m := range mine {
		if strings.TrimSpace(m.Content) == "" || isSuperseded(m) {
			continue
		}
		out = append(out, m)
	}
	// The customer's own facts go first.
	sort.SliceStable(out, func(i, j int) bool {
		return memorySubject(out[i]) == subject && memorySubject(out[j]) != subject
	})
	return out
}

// memorySubject returns the subject_id a hit was written with, "" if none.
// memorus returns the stored payload as a hit's metadata; the caller's own
// metadata sits under its "metadata" key.
func memorySubject(m memorusclient.Memory) string {
	inner, _ := m.Metadata["metadata"].(map[string]any)
	s, _ := inner[MemorySubjectKey].(string)
	return s
}

func isSuperseded(m memorusclient.Memory) bool {
	s, ok := m.Metadata["superseded_by"].(string)
	return ok && s != ""
}

// memoryRelativeFloor drops hits scoring below this fraction of the best hit.
// memorus always returns its top-k, relevant or not; unfiltered, 16 of the 20
// lines a replayed scenario injected into the prompt were unrelated to the
// question. 0.5 was fixed before measuring, not tuned on the scenario.
const memoryRelativeFloor = 0.5

// relevantMemories keeps what is worth putting in front of the LLM: not an
// older value that a later statement replaced (memorus marks it
// `superseded_by` and keeps it only as history), not an empty hit, and not a
// hit far below the best one.
func relevantMemories(ms []memorusclient.Memory) []memorusclient.Memory {
	var top float64
	for _, m := range ms {
		if m.Score > top {
			top = m.Score
		}
	}
	out := make([]memorusclient.Memory, 0, len(ms))
	for _, m := range ms {
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		if isSuperseded(m) {
			continue
		}
		if top > 0 && m.Score < memoryRelativeFloor*top {
			continue
		}
		out = append(out, m)
	}
	return out
}

// memoryTextMaxRunes bounds one remembered statement. Counted in runes: the
// old byte cut split Chinese characters and wrote invalid UTF-8.
const memoryTextMaxRunes = 500

// BuildMemorySummary returns what to remember from one chat turn: the user's
// own words, which is where the business facts live ("张三要求送货前一天电话
// 确认", "矿泉水进价调整为每箱 35 元"). memorus deduplicates repeats and lets a
// changed value supersede the old one, so the text is stored as said.
//
// Returns "" (nothing to remember) for a pure question — it states no fact.
// The tenant goes into the write's metadata, not into the text: in the text it
// polluted matching and was echoed into every prompt.
func BuildMemorySummary(_ uuid.UUID, userMsg, _ string) string {
	text := strings.TrimSpace(userMsg)
	if text == "" || isPureQuestion(text) {
		return ""
	}
	if r := []rune(text); len(r) > memoryTextMaxRunes {
		text = string(r[:memoryTextMaxRunes])
	}
	return text
}

func isPureQuestion(text string) bool {
	return strings.HasSuffix(text, "？") || strings.HasSuffix(text, "?")
}

// MemorySubjectKey is the memorus metadata key naming who a memory is about.
// memorus only compares (dedups / supersedes) memories with the same subject.
const MemorySubjectKey = "subject_id"

// CustomerSubject is the subject_id of memories about one customer.
func CustomerSubject(partnerID uuid.UUID) string {
	return "partner:" + partnerID.String()
}

// MemoryWriteMeta is the metadata attached to a turn written back to memorus.
// customer is the one customer the turn is about, or nil.
func MemoryWriteMeta(tenantID uuid.UUID, customer *CustomerRef) map[string]any {
	meta := map[string]any{"source": "tally-ai", "tally_tenant_id": tenantID.String()}
	if customer != nil {
		meta[MemorySubjectKey] = CustomerSubject(customer.ID)
	}
	return meta
}

// CustomerResolver finds the customers whose names appear in a message.
type CustomerResolver interface {
	MatchCustomersInText(ctx context.Context, tenantID uuid.UUID, text string) ([]CustomerRef, error)
}

// ResolveCustomer returns the one customer text is about, or nil when it
// names none, several, or a name that several customers share. A name that
// only occurs inside a longer matched name does not count: "张三丰要发票"
// is about 张三丰, not also 张三. Errors resolve to nil — the memory is then
// written untagged and recalled unfiltered, as before.
//
// It runs inside the request (the RLS-scoped connection lives there), not in
// the fire-and-forget write goroutine.
func ResolveCustomer(ctx context.Context, cr CustomerResolver, tenantID uuid.UUID, text string) *CustomerRef {
	if cr == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	cands, err := cr.MatchCustomersInText(ctx, tenantID, text)
	if err != nil {
		return nil
	}
	byName := make(map[string][]CustomerRef)
	for _, c := range cands {
		byName[c.Name] = append(byName[c.Name], c)
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return len([]rune(names[i])) > len([]rune(names[j])) })
	rest := text
	var named []string
	for _, n := range names {
		if strings.Contains(rest, n) {
			named = append(named, n)
			rest = strings.ReplaceAll(rest, n, "\x00")
		}
	}
	if len(named) == 0 {
		// Named only by an address form (老李 / 王老板)?
		if sm, ok := cr.(SurnameMatcher); ok {
			return resolveAliasInText(ctx, sm, tenantID, text)
		}
		return nil
	}
	if len(named) != 1 || len(byName[named[0]]) != 1 {
		return nil
	}
	c := byName[named[0]][0]
	return &c
}

// AsyncWriteMemory fires a goroutine to write a memory summary to memorus.
// It is a no-op when mc is nil or there is nothing to remember. Panics in the
// goroutine are recovered silently so a memorus failure can never crash the
// server.
func AsyncWriteMemory(mc MemoryClient, userID string, summary string, meta map[string]any) {
	if mc == nil || summary == "" {
		return
	}
	go func() {
		defer func() { _ = recover() }()
		// Detach from the request lifecycle (which ends when the HTTP response is
		// sent) but bound the write so a hung memorus cannot leak the goroutine.
		ctx, cancel := context.WithTimeout(context.Background(), asyncMemoryWriteTimeout)
		defer cancel()
		_, _ = mc.Add(ctx, userID, summary, meta)
	}()
}
