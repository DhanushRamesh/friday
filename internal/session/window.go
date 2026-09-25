package session

// DefaultBudget : How much of a session, in bytes of text, is sent to a
// provider when no budget is given. Roughly fifteen thousand tokens.
const DefaultBudget = 60000

// BytesPerToken : How many bytes of English prose a token is taken to be.
//
// An approximation, because counting exactly needs the model's own tokeniser.
// DefaultReserveTokens is what absorbs the error.
const BytesPerToken = 4

// DefaultReserveTokens : How much of a model's context window is left alone
// when no reserve is given, for the system prompt, the tool schemas and the
// reply.
const DefaultReserveTokens = 2048

// MinBudget : The least history a model's context window is taken to leave
// room for. A window smaller than the reserve would otherwise work out as no
// history at all, which is worse than overrunning it by a little.
const MinBudget = 2000

// KeepVerbatim : How many of the most recent messages stay as they were said
// when the earlier ones are condensed.
const KeepVerbatim = 20

// condenseAtPercent : How full a ceiling has to be before the earlier part of
// a session is worth condensing.
const condenseAtPercent = 90

// Limits : The ceilings a session's history has to fit under.
//
// They come from three different places and all of them hold: Count is the
// service's, ContextTokens is the model's, and Bytes is our own.
type Limits struct {
	// Bytes : The most text, in bytes, to send regardless of what the model
	// would allow. Zero selects DefaultBudget.
	Bytes int
	// Count : The most messages the service accepts in one request. Zero
	// means it imposes none.
	Count int
	// ContextTokens : The context window of the model behind the service.
	// Zero means it is unknown and only Bytes applies.
	ContextTokens int
	// ReserveTokens : How much of ContextTokens to leave for the system
	// prompt, the tool schemas and the reply. Zero selects
	// DefaultReserveTokens. Ignored without ContextTokens.
	ReserveTokens int
}

// bytes : The size ceiling, resolved: the smaller of our own budget and what
// the model's context window leaves for history.
func (l Limits) bytes() int {
	budget := l.Bytes
	if budget <= 0 {
		budget = DefaultBudget
	}

	if l.ContextTokens <= 0 {
		return budget
	}

	reserve := l.ReserveTokens
	if reserve <= 0 {
		reserve = DefaultReserveTokens
	}

	room := (l.ContextTokens - reserve) * BytesPerToken
	if room < MinBudget {
		room = MinBudget
	}
	if room < budget {
		return room
	}
	return budget
}

// Summary : The earlier part of a session, condensed.
type Summary struct {
	// Text : The condensation, empty when nothing has been condensed.
	Text string
	// ThroughSeq : The last message Text accounts for.
	ThroughSeq int
}

// covers : Whether the message at seq is already accounted for by s.
func (s Summary) covers(seq int) bool {
	return s.Text != "" && seq <= s.ThroughSeq
}

// Window : What one turn sends to a provider.
type Window struct {
	// Summary : The condensed earlier conversation, empty when there is none.
	Summary string
	// Messages : The recent conversation as it was said, oldest first.
	Messages []Message
}

// Plan : The history to send for a turn.
//
// Messages the summary accounts for are replaced by it; the rest are prepared
// with ForModel and then held under every ceiling, the oldest dropped first.
// Whether the result has to open with a user message is a provider's rule,
// not this one's.
func Plan(messages []Message, s Summary, l Limits) Window {
	tail := make([]Message, 0, len(messages))
	for _, m := range messages {
		if s.covers(m.Seq) {
			continue
		}
		tail = append(tail, m)
	}

	turns := within(ForModel(tail), l.bytes())
	if l.Count > 0 && len(turns) > l.Count {
		turns = turns[len(turns)-l.Count:]
	}

	return Window{Summary: s.Text, Messages: turns}
}

// Due : Whether the earlier part of the session should be condensed, and the
// sequence number to condense through.
//
// It reports true once either ceiling is condenseAtPercent full, leaving
// KeepVerbatim messages uncondensed. The boundary is moved back to the start
// of a turn so that a question is never condensed apart from its answer.
func Due(messages []Message, s Summary, l Limits) (int, bool) {
	pending := make([]Message, 0, len(messages))
	for _, m := range messages {
		if s.covers(m.Seq) {
			continue
		}
		pending = append(pending, m)
	}

	if len(pending) <= KeepVerbatim {
		return 0, false
	}

	spent := 0
	for _, m := range pending {
		spent += len(m.Content)
	}

	full := spent*100 >= l.bytes()*condenseAtPercent
	if l.Count > 0 && len(pending)*100 >= l.Count*condenseAtPercent {
		full = true
	}
	if !full {
		return 0, false
	}

	cut := turnStart(pending, len(pending)-KeepVerbatim)
	if cut <= 0 {
		return 0, false
	}
	return pending[cut-1].Seq, true
}

// turnStart : The index at or before i where a turn begins, or -1 when no
// turn begins there.
func turnStart(messages []Message, i int) int {
	for ; i > 0; i-- {
		if messages[i].Role == User {
			return i
		}
	}
	return -1
}

// within : The most recent messages whose content fits in budget bytes,
// oldest first.
//
// A single message longer than the whole budget is cut to length rather than
// dropped, so that the subject of the turn is never missing entirely.
func within(messages []Message, budget int) []Message {
	spent := 0
	first := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		if spent+len(messages[i].Content) > budget {
			break
		}
		spent += len(messages[i].Content)
		first = i
	}

	if first == len(messages) && len(messages) > 0 {
		last := messages[len(messages)-1]
		last.Content = last.Content[len(last.Content)-budget:]
		return []Message{last}
	}
	return messages[first:]
}
