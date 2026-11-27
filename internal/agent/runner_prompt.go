package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lsegal/aviary/internal/cliagent"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

const maxToolRounds = 200

// promptRun holds the state of one prompt turn run by promptCore.
type promptRun struct {
	r                  *AgentRunner
	ctx                context.Context
	cancel             context.CancelFunc
	message            string
	mediaURLs          []string
	overrides          RunOverrides
	promptMsgID        string
	checkpointPath     string
	persistUserMessage bool
	consumers          []StreamConsumer

	sessionID             string
	turn                  *activeRun
	model                 string
	remainingFallbacks    []string
	provider              llm.Provider
	isCLI                 bool
	tokenRefreshAttempted bool
	usage                 *domain.UsageRecord
	serverStopped         chan struct{}
	stopped               atomic.Bool

	toolClient   ToolClient
	tools        []ToolInfo
	systemPrompt string
	memoryBlock  string

	conversation   []llm.Message
	lastResponseID string
}

// roundOutcome tells the tool loop what to do after a provider round.
type roundOutcome int

const (
	roundOK    roundOutcome = iota // the stream finished normally
	roundRetry                     // retry the round with a refreshed or fallback provider
	roundStop                      // the turn has ended
)

// roundResult collects what one provider stream produced.
type roundResult struct {
	text        strings.Builder
	streamedLen int
	mediaURLs   []string
	nativeCalls []llm.ToolCall
}

func (p *promptRun) run(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	p.cancel = cancel

	p.sessionID = p.r.resolveSessionID(ctx)
	ctx = WithSessionID(ctx, p.sessionID)
	ctx = WithSessionAgentID(ctx, p.r.agent.ID)
	ctx = WithWriteGuard(ctx)
	ctx = WithTurnMedia(ctx, p.mediaURLs)
	p.ctx = ctx
	untrack := trackSessionRun(p.r.agent.ID, p.sessionID, cancel)
	defer untrack()
	p.turn = beginActiveTurn(p.r.agent.ID, p.sessionID)
	defer p.turn.end()

	p.resolveModel()
	p.initUsage()
	defer p.recordUsage()
	p.persistPromptMessage()

	slog.Info("agent: prompt started", "agent", p.r.agent.Name, "model", p.model)

	p.serverStopped = make(chan struct{})
	p.setupCheckpoint()
	if p.checkpointPath != "" {
		// Deferred first, so it runs after the checkpoint is deleted below.
		defer p.r.releaseCheckpoint(p.checkpointPath)
	}
	defer p.finishCheckpoint()
	go p.watchStop()

	// Guard: if this message was already successfully answered (e.g. by a
	// concurrent run or on a retry), do not process it again.
	if p.promptMsgID != "" && HasMessageResponse(p.r.agent.ID, p.sessionID, p.promptMsgID) {
		p.emit(StreamEvent{Type: StreamEventDone})
		return
	}
	if p.provider == nil && !p.isCLI {
		p.reportNoProvider()
		return
	}
	if !p.overrides.Bare {
		toolClient, err := newToolClientFactory(p.ctx)
		if err != nil {
			p.deliverAssistantError(err)
			p.emit(StreamEvent{Type: StreamEventError, Err: err})
			return
		}
		if toolClient != nil {
			defer toolClient.Close() //nolint:errcheck
		}
		p.toolClient = toolClient
		p.buildSystemPrompt()
	}
	p.dispatch()
}

// resolveModel picks the run's model, fallbacks, and provider.
func (p *promptRun) resolveModel() {
	p.model = p.overrides.Model
	if p.model == "" {
		p.model = p.r.agent.Model
	}
	fallbacks := p.overrides.Fallbacks
	if len(fallbacks) == 0 {
		fallbacks = p.r.agent.Fallbacks
	}
	p.remainingFallbacks = make([]string, len(fallbacks))
	copy(p.remainingFallbacks, fallbacks)

	// Resolve the provider from the factory for this prompt to ensure
	// fresh OAuth tokens (factory.ForModel calls resolveOAuthToken).
	p.provider = p.r.provider
	if p.r.factory != nil {
		if prov, err := p.r.factory.ForModel(p.model); err == nil {
			p.provider = prov
		}
	}
	// CLI-backed models hand the whole turn to a coding-agent CLI.
	p.isCLI = cliagent.IsCLIModel(p.model)
}

func (p *promptRun) tryFallback(origErr error) bool {
	for len(p.remainingFallbacks) > 0 && p.r.factory != nil {
		nextModel := p.remainingFallbacks[0]
		p.remainingFallbacks = p.remainingFallbacks[1:]
		prov, err := p.r.factory.ForModel(nextModel)
		if err != nil {
			slog.Warn("agent: fallback provider failed", "agent", p.r.agent.Name, "model", nextModel, "err", err)
			continue
		}
		slog.Info("agent: falling back to model", "agent", p.r.agent.Name, "from", p.model, "to", nextModel, "reason", origErr)
		p.model = nextModel
		p.provider = prov
		return true
	}
	return false
}

// tryTokenRefresh attempts to refresh the OAuth token for the current model
// after receiving a 401. Only one refresh attempt is made per prompt.
func (p *promptRun) tryTokenRefresh(origErr error) bool {
	if p.r.factory == nil || !isAuthError(origErr) {
		return false
	}
	if p.tokenRefreshAttempted {
		slog.Debug("agent: token refresh already attempted, skipping", "agent", p.r.agent.Name, "err", origErr)
		return false
	}
	p.tokenRefreshAttempted = true
	prov, err := p.r.factory.ForModelForceRefresh(p.model)
	if err != nil {
		slog.Warn("agent: token refresh failed after 401", "agent", p.r.agent.Name, "err", err)
		return false
	}
	slog.Info("agent: refreshed OAuth token after 401, retrying", "agent", p.r.agent.Name, "model", p.model)
	p.provider = prov
	return true
}

// initUsage starts the usage record, accumulated across all rounds.
func (p *promptRun) initUsage() {
	p.usage = &domain.UsageRecord{
		SessionID: p.sessionID,
		AgentID:   p.r.agent.ID,
		Model:     p.model,
		Provider:  extractProvider(p.model),
		Channel:   UsageChannelFromContext(p.ctx),
	}
	if taskID, ok := TaskIDFromContext(p.ctx); ok {
		p.usage.Task = taskID
	}
}

func (p *promptRun) recordUsage() {
	u := p.usage
	if u.InputTokens == 0 && u.OutputTokens == 0 && !u.HasError && !u.HasThrottle {
		return
	}
	u.Timestamp = time.Now()
	snapshotUsageSession(p.ctx, u)
	if err := store.AppendJSONL(store.UsagePath(), u); err != nil {
		slog.Warn("agent: failed to record usage", "err", err)
	}
}

func (p *promptRun) persistPromptMessage() {
	if !p.persistUserMessage {
		return
	}
	if p.overrides.Retry {
		p.promptMsgID = UnansweredUserMessageID(p.r.agent.ID, p.sessionID, p.message, p.mediaURLs)
	}
	if p.promptMsgID == "" {
		var userSender *domain.MessageSender
		if sender, ok := SessionSenderFromContext(p.ctx); ok {
			userSender = sender
		}
		p.promptMsgID = p.r.appendSessionMessageWithSender(p.sessionID, domain.MessageRoleUser, p.message, p.mediaURLs, p.model, userSender)
	}
}

// setupCheckpoint writes and claims the run checkpoint. Checkpoints are
// written only for interactive prompts: scheduled jobs already have
// queue-level retry semantics, and replaying their session checkpoints on
// every reconcile creates noisy duplicate recovery loops.
func (p *promptRun) setupCheckpoint() {
	if p.checkpointPath != "" {
		p.r.claimCheckpoint(p.checkpointPath)
		return
	}
	_, isScheduledTaskRun := TaskIDFromContext(p.ctx)
	if p.promptMsgID == "" || isScheduledTaskRun {
		return
	}
	cp := &RunCheckpoint{
		AgentName: p.r.agent.Name,
		SessionID: p.sessionID,
		Message:   p.message,
		MediaURLs: p.mediaURLs,
		Overrides: p.overrides,
		CreatedAt: time.Now(),
	}
	cpath := store.CheckpointPath(p.r.agent.ID, p.promptMsgID)
	// Claim it before the file exists, so recovery never finds it unclaimed.
	p.r.claimCheckpoint(cpath)
	if err := store.WriteJSON(cpath, cp); err != nil {
		slog.Warn("agent: failed to write run checkpoint", "agent", p.r.agent.Name, "err", err)
		p.r.releaseCheckpoint(cpath)
		return
	}
	p.checkpointPath = cpath
}

// finishCheckpoint deletes the checkpoint unless the server stopped this
// runner, in which case it is kept for recovery on restart.
func (p *promptRun) finishCheckpoint() {
	if p.checkpointPath == "" {
		return
	}
	select {
	case <-p.serverStopped:
		// Server-initiated stop: keep checkpoint for recovery on restart.
	default:
		// Normal completion, user-stop, or provider error: remove checkpoint.
		if err := store.DeleteJSON(p.checkpointPath); err != nil {
			slog.Warn("agent: failed to delete run checkpoint", "agent", p.r.agent.Name, "err", err)
		}
	}
}

// watchStop cancels the turn when the runner is stopped.
func (p *promptRun) watchStop() {
	select {
	case <-p.r.stopCh:
		close(p.serverStopped)
		p.cancel()
	case <-p.ctx.Done():
	}
}

// emit sends e to the turn's consumers. A turn reports its stop once,
// however many of its code paths notice the cancellation. A stop the
// runner's shutdown caused, not a stop request, is marked Interrupted.
func (p *promptRun) emit(e StreamEvent) {
	e.AgentID = p.r.agent.ID
	if e.Type == StreamEventStop {
		if !p.stopped.CompareAndSwap(false, true) {
			return
		}
		select {
		case <-p.serverStopped:
			e.Interrupted = p.r.wasInterrupted()
		default:
		}
	}
	p.turn.record(e)
	for _, c := range p.consumers {
		c(e)
	}
}

func (p *promptRun) emitCanceled() {
	p.emit(StreamEvent{Type: StreamEventStop})
}

func (p *promptRun) deliver(text string) {
	if !p.overrides.SuppressDelivery {
		deliverToSession(p.r.agent.ID, p.sessionID, text)
	}
}

func (p *promptRun) deliverAssistantError(err error) {
	if err == nil {
		return
	}
	// Do NOT persist API errors as assistant messages — they would be
	// re-sent in subsequent requests and perpetuate the failure loop.
	p.deliver(fmt.Sprintf("Error: %v", err))
}

// reportNoProvider surfaces a missing LLM provider as a message so the UI
// shows it but tests pass.
func (p *promptRun) reportNoProvider() {
	if p.ctx.Err() != nil {
		p.emitCanceled()
		return
	}
	slog.Warn("agent: no provider", "agent", p.r.agent.Name, "model", p.model)
	msg := fmt.Sprintf("[no LLM provider configured for %q — check credentials and model settings]", p.model)
	p.r.appendSessionMessage(p.sessionID, domain.MessageRoleAssistant, msg, nil, p.model)
	p.emit(StreamEvent{Type: StreamEventText, Text: msg})
	p.deliver(msg)
	p.emit(StreamEvent{Type: StreamEventDone})
}

// dispatch runs the turn on a CLI or through the provider's tool loop.
func (p *promptRun) dispatch() {
	useHistory := true
	if p.overrides.History != nil {
		useHistory = *p.overrides.History
	}

	// The model sees text attachments inline; the stored message keeps them
	// as attachments.
	modelMessage, modelMedia := llm.InlineTextAttachments(p.message, p.mediaURLs)
	modelMessage, modelMedia = resolveFileAttachments(modelMessage, modelMedia, p.isCLI)

	if !p.isCLI {
		p.runToolLoop(modelMessage, modelMedia, useHistory)
		return
	}
	p.r.runCLITurn(p.ctx, cliTurnInput{
		model:         p.model,
		message:       modelMessage,
		mediaURLs:     modelMedia,
		memoryContext: p.memoryBlock,
		systemPrompt:  p.systemPrompt,
		tools:         p.tools,
		toolClient:    p.toolClient,
		sessionID:     p.sessionID,
		promptMsgID:   p.promptMsgID,
		useHistory:    useHistory,
		usage:         p.usage,
		emit:          p.emit,
		deliver:       p.deliver,
		deliverError:  p.deliverAssistantError,
	})
}

// runToolLoop streams provider rounds, running requested tools between them,
// until the model answers.
func (p *promptRun) runToolLoop(modelMessage string, modelMedia []string, useHistory bool) {
	metaPath := store.SessionMetaPath(p.r.agent.ID, p.sessionID)
	conversationID := p.initConversation(metaPath, modelMessage, modelMedia, useHistory)
	toolNames := make(map[string]struct{}, len(p.tools))
	for _, t := range p.tools {
		toolNames[t.Name] = struct{}{}
	}
	llmTools := buildLLMToolDefinitions(p.tools)

	for round := 0; round < maxToolRounds; round++ {
		if p.ctx.Err() != nil {
			p.emitCanceled()
			return
		}
		req := llm.Request{
			Model:        p.model,
			Messages:     p.conversation,
			System:       p.systemPrompt,
			Stream:       true,
			CacheControl: llm.DefaultPromptCacheControl(),
			Tools:        llmTools,
		}
		// On the first round, pass the conversation ID so the provider can
		// resume server-side history without replaying all prior messages.
		if round == 0 && conversationID != "" {
			req.PreviousResponseID = conversationID
		}
		switch p.runRound(p.fitRequestToBudget(req), toolNames, metaPath) {
		case roundRetry:
			round--
		case roundStop:
			return
		}
	}

	errMsg := fmt.Sprintf("Error: tool loop exceeded %d rounds", maxToolRounds)
	p.r.appendSessionMessage(p.sessionID, domain.MessageRoleAssistant, errMsg, nil, p.model)
	p.usage.HasError = true
	p.deliver(errMsg)
	p.emit(StreamEvent{Type: StreamEventError, Err: fmt.Errorf("tool loop exceeded %d rounds", maxToolRounds)})
}

// runRound streams one round and runs the tools it requests. roundOK means
// the loop goes on to the next round.
func (p *promptRun) runRound(req llm.Request, toolNames map[string]struct{}, metaPath string) roundOutcome {
	res, outcome := p.streamRound(req)
	if outcome != roundOK {
		return outcome
	}
	if p.ctx.Err() != nil {
		p.emitCanceled()
		return roundStop
	}
	answer := strings.TrimSpace(res.text.String())
	if answer != "" && res.streamedLen == 0 {
		p.emit(StreamEvent{Type: StreamEventText, Text: answer})
	}
	if len(res.nativeCalls) > 0 && p.toolClient != nil {
		if canceled := p.runNativeToolCalls(answer, res.nativeCalls, toolNames); canceled {
			return roundStop
		}
		return roundOK
	}
	p.finishAnswer(answer, res.mediaURLs, metaPath)
	return roundStop
}

// initConversation seeds the conversation and returns any server-side
// conversation ID to resume. A valid (non-expired) ID lets the provider resume
// server-side history so only the new user turn is sent. Task sessions never
// use conversation IDs.
func (p *promptRun) initConversation(metaPath, modelMessage string, modelMedia []string, useHistory bool) string {
	p.conversation = []llm.Message{{Role: llm.RoleUser, Content: modelMessage, MediaURLs: modelMedia}}
	if !useHistory {
		return ""
	}
	conversationID := ""
	if !p.r.isTaskSession(p.sessionID) {
		conversationID = resumableConversationID(metaPath)
	}
	if conversationID != "" {
		return conversationID
	}
	// No valid server-side conversation ID — fall back to persisted session
	// history only. Non-triggering group messages are now written directly
	// into the session JSONL with participant metadata.
	history := p.r.loadSessionConversationWithinBudget(p.sessionID, 24, llm.ModelInputBudget(p.model), p.systemPrompt)
	if len(history) > 0 {
		// The last history message is this prompt, which still needs its
		// attachments.
		last := &history[len(history)-1]
		last.Content += strings.TrimPrefix(modelMessage, p.message)
		last.MediaURLs = modelMedia
		p.conversation = history
	}
	return ""
}

// resumableConversationID returns the session's provider conversation ID when
// it was used within the last six hours.
func resumableConversationID(metaPath string) string {
	meta, err := store.ReadSessionMeta(metaPath)
	if err != nil || meta.Conversation == nil || meta.Conversation.ID == "" {
		return ""
	}
	if time.Since(meta.Conversation.LastUsedAt) >= 6*time.Hour {
		return ""
	}
	return meta.Conversation.ID
}

// fitRequestToBudget ensures req fits within the model's input token budget.
// While it is over budget, the oldest messages are summarized in chunks
// (preserving content) rather than dropped; if summarizing fails or does not
// shrink it enough, it is compacted.
func (p *promptRun) fitRequestToBudget(req llm.Request) llm.Request {
	budget := llm.ModelInputBudget(p.model)
	if llm.EstimateRequestTokens(req) <= budget {
		return req
	}
	slog.Warn("agent: request exceeds input token budget; summarizing", "agent", p.r.agent.Name, "model", p.model)
	for llm.EstimateRequestTokens(req) > budget && len(req.Messages) > 1 {
		messages, ok := p.summarizeOldest(req.Messages)
		if !ok {
			req = llm.CompactToTokenBudget(req, budget)
			break
		}
		req.Messages = messages
	}
	if llm.EstimateRequestTokens(req) > budget {
		slog.Warn("agent: summarized request still exceeds input token budget; compacting", "agent", p.r.agent.Name, "model", p.model)
		req = llm.CompactToTokenBudget(req, budget)
	}
	return req
}

// summarizeOldest replaces the older half of messages with a single summary
// message. It reports false when summarization fails.
func (p *promptRun) summarizeOldest(messages []llm.Message) ([]llm.Message, bool) {
	chunk := max(len(messages)/2, 1)
	old := make([]llm.Message, chunk)
	copy(old, messages[:chunk])
	summary, err := llm.SummarizeMessages(p.ctx, p.provider, p.model, old)
	if err != nil || strings.TrimSpace(summary) == "" {
		slog.Warn("agent: summarization failed; falling back to drop", "err", err)
		return nil, false
	}
	summaryMsg := llm.Message{Role: llm.RoleUser, Content: "[Summarized earlier conversation]:\n" + summary}
	out := make([]llm.Message, 0, 1+len(messages)-chunk)
	out = append(out, summaryMsg)
	return append(out, messages[chunk:]...), true
}

// streamRound streams one provider response.
func (p *promptRun) streamRound(req llm.Request) (*roundResult, roundOutcome) {
	res := &roundResult{}
	ch, err := p.provider.Stream(p.ctx, req)
	if err != nil {
		return res, p.handleProviderError(err, true)
	}
	for event := range ch {
		outcome, done := p.handleStreamEvent(event, res)
		if !done {
			continue
		}
		if outcome == roundRetry {
			for ev := range ch {
				_ = ev
			}
		}
		return res, outcome
	}
	return res, roundOK
}

// handleStreamEvent applies one stream event to res. It reports done when
// the event ends the round early.
func (p *promptRun) handleStreamEvent(event llm.Event, res *roundResult) (roundOutcome, bool) {
	switch event.Type {
	case llm.EventTypeText:
		res.text.WriteString(event.Text)
		res.streamedLen += len(event.Text)
		p.emit(StreamEvent{Type: StreamEventText, Text: event.Text})
	case llm.EventTypeMedia:
		if event.MediaURL != "" {
			res.mediaURLs = append(res.mediaURLs, event.MediaURL)
			p.emit(StreamEvent{Type: StreamEventMedia, MediaURL: event.MediaURL})
		}
	case llm.EventTypeUsage:
		p.addUsage(event.Usage)
	case llm.EventTypeToolCall:
		if event.ToolCall != nil {
			res.nativeCalls = append(res.nativeCalls, *event.ToolCall)
		}
	case llm.EventTypeError:
		return p.handleProviderError(event.Error, false), true
	case llm.EventTypeDone:
		if event.ResponseID != "" {
			p.lastResponseID = event.ResponseID
		}
	}
	return roundOK, false
}

func (p *promptRun) addUsage(u *llm.Usage) {
	if u == nil {
		return
	}
	p.usage.InputTokens += u.InputTokens
	p.usage.OutputTokens += u.OutputTokens
	p.usage.CacheReadTokens += u.CacheReadTokens
	p.usage.CacheWriteTokens += u.CacheWriteTokens
}

// handleProviderError handles a provider error, retrying with a refreshed
// token or fallback model when possible. logErr logs a stream start failure.
func (p *promptRun) handleProviderError(err error, logErr bool) roundOutcome {
	if errors.Is(err, context.Canceled) || p.ctx.Err() != nil {
		p.emitCanceled()
		return roundStop
	}
	markUsageFailure(p.usage, err)
	if isRetryableError(err) && (p.tryTokenRefresh(err) || p.tryFallback(err)) {
		return roundRetry
	}
	if logErr {
		slog.Error("agent: stream error", "agent", p.r.agent.Name, "err", err)
	}
	p.deliverAssistantError(err)
	p.emit(StreamEvent{Type: StreamEventError, Err: err})
	return roundStop
}

// runNativeToolCalls runs the model's requested tool calls and appends them
// and their results to the conversation. It reports whether the turn was
// canceled.
func (p *promptRun) runNativeToolCalls(answer string, calls []llm.ToolCall, toolNames map[string]struct{}) bool {
	assistantContent := answer
	for _, call := range calls {
		if _, exists := toolNames[call.Name]; !exists {
			p.conversation = append(p.conversation,
				llm.Message{Role: llm.RoleAssistant, Content: answer},
				llm.Message{Role: llm.RoleUser, Content: fmt.Sprintf("Tool %q is not available. Choose one of the available tools.", call.Name)},
			)
			return false
		}
		resultText, canceled := p.r.executeToolCall(
			p.ctx,
			p.emit,
			p.toolClient,
			p.sessionID,
			p.usage,
			toolEventRecord{Name: call.Name, Args: call.Arguments},
			call.Name,
			call.Arguments,
		)
		if canceled {
			return true
		}
		if assistantContent != "" {
			p.conversation = append(p.conversation, llm.Message{Role: llm.RoleAssistant, Content: assistantContent})
			assistantContent = ""
		}
		p.appendToolExchange(call, resultText)
	}
	return false
}

func (p *promptRun) appendToolExchange(call llm.ToolCall, resultText string) {
	callID := strings.TrimSpace(call.ID)
	if callID == "" {
		callID = call.Name
	}
	p.conversation = append(p.conversation,
		llm.Message{Role: llm.RoleAssistant, ToolCall: &llm.ToolCall{
			ID:        callID,
			Name:      call.Name,
			Arguments: call.Arguments,
		}},
		llm.Message{Role: llm.RoleUser, Result: &llm.ToolResult{
			ToolCallID: callID,
			Name:       call.Name,
			Content:    resultText,
		}},
	)
}

// finishAnswer persists the final answer and any returned images, marks the
// prompt responded, and saves the provider's response ID for continuity.
func (p *promptRun) finishAnswer(answer string, mediaURLs []string, metaPath string) {
	// Persist each returned image as a separate assistant message.
	for _, mURL := range mediaURLs {
		p.r.appendSessionMessage(p.sessionID, domain.MessageRoleAssistant, "", []string{mURL}, p.model)
	}
	var assistantMsgID string
	if answer != "" {
		assistantMsgID = p.r.appendSessionMessage(p.sessionID, domain.MessageRoleAssistant, answer, nil, p.model)
	}
	slog.Info("agent: prompt done", "agent", p.r.agent.Name, "model", p.model)
	p.markResponded(assistantMsgID)
	p.saveResponseID(metaPath)
	p.deliver(answer)
	p.emit(StreamEvent{Type: StreamEventDone})
}

// markResponded marks the user message as responded so it is never
// processed twice.
func (p *promptRun) markResponded(assistantMsgID string) {
	if p.promptMsgID == "" {
		return
	}
	if assistantMsgID == "" {
		assistantMsgID = newID("resp")
	}
	if err := MarkMessageResponded(p.r.agent.ID, p.sessionID, p.promptMsgID, assistantMsgID); err != nil {
		slog.Warn("agent: failed to mark message responded", "session", p.sessionID, "err", err)
	}
}

func (p *promptRun) saveResponseID(metaPath string) {
	if p.lastResponseID == "" {
		return
	}
	err := store.UpdateSessionMeta(metaPath, func(meta *store.SessionMeta) {
		meta.Conversation = &store.ConversationMeta{
			ID:         p.lastResponseID,
			LastUsedAt: time.Now(),
		}
	})
	if err != nil {
		slog.Warn("agent: failed to save session meta", "session", p.sessionID, "err", err)
	}
}
