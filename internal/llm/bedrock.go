package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brdocument "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const defaultBedrockRegion = "us-east-1"

// BedrockProvider streams responses from AWS Bedrock via the Converse API.
// Authentication uses the standard AWS credential chain (env vars, shared
// config, instance profiles, etc.). An explicit region can be set via
// ProviderOptions; otherwise us-east-1 is used.
type BedrockProvider struct {
	client    *bedrockruntime.Client
	awsCfg    aws.Config
	model     string
	region    string
	accountID string
	resolved  bool
}

// NewBedrockProvider creates a provider backed by AWS Bedrock's ConverseStream API.
// region is optional (defaults to us-east-1). profile is optional (AWS named profile).
// accessKey/secretKey are optional; when empty the default AWS credential chain is used.
func NewBedrockProvider(ctx context.Context, model, region, profile, accessKey, secretKey string) (*BedrockProvider, error) {
	if region == "" {
		region = defaultBedrockRegion
	}
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}
	if accessKey != "" && secretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("bedrock: loading AWS config: %w", err)
	}

	return &BedrockProvider{
		client: bedrockruntime.NewFromConfig(cfg),
		awsCfg: cfg,
		model:  model,
		region: region,
	}, nil
}

// resolveModelID returns the model identifier to pass to the Converse API.
// Cross-region inference profiles (prefixed with us., eu., ap.) require a
// full ARN; foundation model IDs are used as-is. The account ID is resolved
// lazily via STS on first call.
func (p *BedrockProvider) resolveModelID(ctx context.Context) (string, error) {
	if !needsInferenceProfileARN(p.model) {
		return p.model, nil
	}
	if !p.resolved {
		result, err := sts.NewFromConfig(p.awsCfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			return "", fmt.Errorf("bedrock: resolving account ID for inference profile ARN: %w", err)
		}
		p.accountID = aws.ToString(result.Account)
		p.resolved = true
	}
	return fmt.Sprintf("arn:aws:bedrock:%s:%s:inference-profile/%s", p.region, p.accountID, p.model), nil
}

// needsInferenceProfileARN returns true for model IDs that are cross-region
// inference profiles (e.g. us.anthropic.claude-*, eu.meta.llama-*).
func needsInferenceProfileARN(model string) bool {
	prefixes := []string{"us.", "eu.", "ap.", "global."}
	for _, prefix := range prefixes {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}

// newBedrockProviderWithClient is a test helper that injects a pre-built client.
func newBedrockProviderWithClient(client *bedrockruntime.Client, model, region string) *BedrockProvider {
	if region == "" {
		region = defaultBedrockRegion
	}
	return &BedrockProvider{client: client, model: model, region: region, accountID: "123456789012", resolved: true}
}

// Ping validates credentials by sending a minimal Converse request.
func (p *BedrockProvider) Ping(ctx context.Context) error {
	modelID, err := p.resolveModelID(ctx)
	if err != nil {
		return err
	}
	_, err = p.client.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId: aws.String(modelID),
		Messages: []brtypes.Message{{
			Role:    brtypes.ConversationRoleUser,
			Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: "Hi"}},
		}},
		InferenceConfig: &brtypes.InferenceConfiguration{
			MaxTokens: aws.Int32(1),
		},
	})
	return err
}

// Stream sends a conversation to Bedrock's ConverseStream API and returns
// events on a channel. The channel is closed after EventTypeDone or
// EventTypeError.
func (p *BedrockProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	input, err := p.buildConverseInput(ctx, req)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.ConverseStream(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock converse stream: %w", err)
	}

	ch := make(chan Event, 32)
	go p.consumeStream(resp, ch)
	return ch, nil
}

func (p *BedrockProvider) buildConverseInput(ctx context.Context, req Request) (*bedrockruntime.ConverseStreamInput, error) {
	modelID, err := p.resolveModelID(ctx)
	if err != nil {
		return nil, err
	}
	messages, err := bedrockMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	input := &bedrockruntime.ConverseStreamInput{
		ModelId:  aws.String(modelID),
		Messages: messages,
	}
	if req.System != "" {
		input.System = []brtypes.SystemContentBlock{
			&brtypes.SystemContentBlockMemberText{Value: req.System},
		}
	}
	if req.MaxToks > 0 {
		input.InferenceConfig = &brtypes.InferenceConfiguration{
			MaxTokens: aws.Int32(int32(req.MaxToks)),
		}
	}
	if len(req.Tools) > 0 {
		tc := &brtypes.ToolConfiguration{}
		for _, t := range req.Tools {
			schema := bedrockToolSchema(t.InputSchema)
			tc.Tools = append(tc.Tools, &brtypes.ToolMemberToolSpec{
				Value: brtypes.ToolSpecification{
					Name:        aws.String(t.Name),
					Description: aws.String(t.Description),
					InputSchema: &brtypes.ToolInputSchemaMemberJson{Value: schema},
				},
			})
		}
		input.ToolConfig = tc
	}
	return input, nil
}

func (p *BedrockProvider) consumeStream(resp *bedrockruntime.ConverseStreamOutput, ch chan<- Event) {
	defer close(ch)

	type pendingToolCall struct {
		ID   string
		Name string
		JSON strings.Builder
	}
	var (
		lastUsage    *Usage
		pendingCalls []*pendingToolCall
		current      *pendingToolCall
	)

	stream := resp.GetStream()
	defer stream.Close() //nolint:errcheck

	for event := range stream.Events() {
		switch e := event.(type) {
		case *brtypes.ConverseStreamOutputMemberContentBlockDelta:
			switch delta := e.Value.Delta.(type) {
			case *brtypes.ContentBlockDeltaMemberText:
				if delta.Value != "" {
					ch <- Event{Type: EventTypeText, Text: delta.Value}
				}
			case *brtypes.ContentBlockDeltaMemberToolUse:
				if current != nil {
					current.JSON.WriteString(aws.ToString(delta.Value.Input))
				}
			}

		case *brtypes.ConverseStreamOutputMemberContentBlockStart:
			if start, ok := e.Value.Start.(*brtypes.ContentBlockStartMemberToolUse); ok {
				tc := &pendingToolCall{
					ID:   aws.ToString(start.Value.ToolUseId),
					Name: aws.ToString(start.Value.Name),
				}
				pendingCalls = append(pendingCalls, tc)
				current = tc
			}

		case *brtypes.ConverseStreamOutputMemberContentBlockStop:
			current = nil

		case *brtypes.ConverseStreamOutputMemberMetadata:
			if e.Value.Usage != nil {
				lastUsage = &Usage{
					InputTokens:  int(aws.ToInt32(e.Value.Usage.InputTokens)),
					OutputTokens: int(aws.ToInt32(e.Value.Usage.OutputTokens)),
				}
			}

		case *brtypes.ConverseStreamOutputMemberMessageStop:
			// End of message; emit tool calls if any, then usage + done.
		}
	}

	if err := stream.Err(); err != nil {
		ch <- Event{Type: EventTypeError, Error: fmt.Errorf("bedrock stream: %w", err)}
		return
	}

	for _, call := range pendingCalls {
		if strings.TrimSpace(call.Name) == "" {
			continue
		}
		args, err := parseToolArguments(call.JSON.String())
		if err != nil {
			ch <- Event{Type: EventTypeError, Error: fmt.Errorf("bedrock tool arguments for %q: %w", call.Name, err)}
			return
		}
		ch <- Event{Type: EventTypeToolCall, ToolCall: &ToolCall{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: args,
		}}
	}

	if lastUsage != nil {
		ch <- Event{Type: EventTypeUsage, Usage: lastUsage}
	}
	ch <- Event{Type: EventTypeDone}
}

// bedrockMessages converts the internal Message slice to Bedrock Converse
// messages, merging consecutive same-role messages (Bedrock requires
// user/assistant alternation).
func bedrockMessages(msgs []Message) ([]brtypes.Message, error) {
	var out []brtypes.Message
	appendMsg := func(role brtypes.ConversationRole, blocks []brtypes.ContentBlock) {
		if len(out) > 0 && out[len(out)-1].Role == role {
			out[len(out)-1].Content = append(out[len(out)-1].Content, blocks...)
			return
		}
		out = append(out, brtypes.Message{Role: role, Content: blocks})
	}

	for _, m := range msgs {
		switch m.Role {
		case RoleUser:
			if m.Result != nil {
				appendMsg(brtypes.ConversationRoleUser, []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberToolResult{Value: brtypes.ToolResultBlock{
						ToolUseId: aws.String(m.Result.ToolCallID),
						Content: []brtypes.ToolResultContentBlock{
							&brtypes.ToolResultContentBlockMemberText{Value: m.Result.Content},
						},
						Status: bedrockToolStatus(m.Result.IsError),
					}},
				})
				continue
			}
			blocks := []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: m.Content},
			}
			appendMsg(brtypes.ConversationRoleUser, blocks)

		case RoleAssistant:
			if m.ToolCall != nil {
				appendMsg(brtypes.ConversationRoleAssistant, []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberToolUse{Value: brtypes.ToolUseBlock{
						ToolUseId: aws.String(m.ToolCall.ID),
						Name:      aws.String(m.ToolCall.Name),
						Input:     brdocument.NewLazyDocument(m.ToolCall.Arguments),
					}},
				})
				continue
			}
			appendMsg(brtypes.ConversationRoleAssistant, []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: m.Content},
			})

		case RoleSystem:
			appendMsg(brtypes.ConversationRoleUser, []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: m.Content},
			})
		}
	}
	return out, nil
}

func bedrockToolStatus(isError bool) brtypes.ToolResultStatus {
	if isError {
		return brtypes.ToolResultStatusError
	}
	return brtypes.ToolResultStatusSuccess
}

func bedrockToolSchema(schema any) brdocument.Interface {
	obj := schemaObject(schema)
	if obj == nil {
		obj = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return brdocument.NewLazyDocument(obj)
}
