# Fantasy LLM Library Reference

Source: `charm.land/fantasy@v0.17.1`

Package fantasy provides a unified interface for interacting with various AI language models.

## Provider Interface

```go
type Provider interface {
    Name() string
    LanguageModel(ctx context.Context, modelID string) (LanguageModel, error)
}
```

## LanguageModel Interface

```go
type LanguageModel interface {
    Generate(context.Context, Call) (*Response, error)
    Stream(context.Context, Call) (StreamResponse, error)
    GenerateObject(context.Context, ObjectCall) (*ObjectResponse, error)
    StreamObject(context.Context, ObjectCall) (ObjectStreamResponse, error)
    Provider() string
    Model() string
}
```

## Agent Interface

```go
type Agent interface {
    Generate(context.Context, AgentCall) (*AgentResult, error)
    Stream(context.Context, AgentStreamCall) (*AgentResult, error)
}
```

Created via `NewAgent(model LanguageModel, opts ...AgentOption)`.

## Message Types

### MessageRole

```go
const (
    MessageRoleSystem    MessageRole = "system"
    MessageRoleUser      MessageRole = "user"
    MessageRoleAssistant MessageRole = "assistant"
    MessageRoleTool      MessageRole = "tool"
)
```

### Message

```go
type Message struct {
    Role            MessageRole
    Content         []MessagePart
    ProviderOptions ProviderOptions
}
```

Constructors:
- `NewUserMessage(prompt string, files ...FilePart) Message`
- `NewSystemMessage(prompt ...string) Message`

### MessagePart Interface

```go
type MessagePart interface {
    GetType() ContentType
    Options() ProviderOptions
}
```

### MessagePart Types

**TextPart:**
```go
type TextPart struct {
    Text            string
    ProviderOptions ProviderOptions
}
// GetType() -> ContentTypeText
```

**ReasoningPart:**
```go
type ReasoningPart struct {
    Text            string
    ProviderOptions ProviderOptions
}
// GetType() -> ContentTypeReasoning
```

**FilePart:**
```go
type FilePart struct {
    Filename        string
    Data            []byte
    MediaType       string
    ProviderOptions ProviderOptions
}
// GetType() -> ContentTypeFile
```

**ToolCallPart:**
```go
type ToolCallPart struct {
    ToolCallID       string
    ToolName         string
    Input            string          // JSON string
    ProviderExecuted bool
    ProviderOptions  ProviderOptions
}
// GetType() -> ContentTypeToolCall
```

**ToolResultPart:**
```go
type ToolResultPart struct {
    ToolCallID       string
    Output           ToolResultOutputContent
    ProviderExecuted bool
    ProviderOptions  ProviderOptions
}
// GetType() -> ContentTypeToolResult
```

### Generic Conversion

```go
func AsMessagePart[T MessagePart](content MessagePart) (T, bool)
```

## Content Types (Response)

### ContentType

```go
const (
    ContentTypeText       ContentType = "text"
    ContentTypeReasoning  ContentType = "reasoning"
    ContentTypeFile       ContentType = "file"
    ContentTypeSource     ContentType = "source"
    ContentTypeToolCall   ContentType = "tool-call"
    ContentTypeToolResult ContentType = "tool-result"
)
```

### Content Interface

```go
type Content interface {
    GetType() ContentType
}
```

### Content Types

**TextContent:**
```go
type TextContent struct {
    Text             string
    ProviderMetadata ProviderMetadata
}
```

**ReasoningContent:**
```go
type ReasoningContent struct {
    Text             string
    ProviderMetadata ProviderMetadata
}
```

**FileContent:**
```go
type FileContent struct {
    MediaType        string   // IANA media type
    Data             []byte
    ProviderMetadata ProviderMetadata
}
```

**SourceContent:**
```go
type SourceContent struct {
    SourceType       SourceType  // "url" or "document"
    ID               string
    URL              string      // for URL sources
    Title            string
    MediaType        string      // for document sources
    Filename         string      // for document sources
    ProviderMetadata ProviderMetadata
}
```

**ToolCallContent:**
```go
type ToolCallContent struct {
    ToolCallID       string
    ToolName         string
    Input            string        // JSON string
    ProviderExecuted bool
    ProviderMetadata ProviderMetadata
    Invalid          bool          // Failed validation
    ValidationError  error         // Only set if Invalid
}
```

**ToolResultContent:**
```go
type ToolResultContent struct {
    ToolCallID       string
    ToolName         string
    Result           ToolResultOutputContent
    ClientMetadata   string
    ProviderExecuted bool
    ProviderMetadata ProviderMetadata
}
```

### Generic Conversion

```go
func AsContentType[T Content](content Content) (T, bool)
```

### ResponseContent

```go
type ResponseContent []Content
```

Methods: `Text()`, `Reasoning()`, `ReasoningText()`, `Files()`, `Sources()`, `ToolCalls()`, `ToolResults()`

## Tool Types

### AgentTool Interface

```go
type AgentTool interface {
    Info() ToolInfo
    Run(ctx context.Context, params ToolCall) (ToolResponse, error)
    ProviderOptions() ProviderOptions
    SetProviderOptions(opts ProviderOptions)
}
```

### ToolInfo

```go
type ToolInfo struct {
    Name        string
    Description string
    Parameters  map[string]any  // JSON Schema
    Required    []string
    Parallel    bool            // Safe for parallel execution
}
```

### ToolCall

```go
type ToolCall struct {
    ID    string
    Name  string
    Input string  // JSON string
}
```

### ToolResponse

```go
type ToolResponse struct {
    Type      string
    Content   string
    Data      []byte   // Binary data for media
    MediaType string   // MIME type
    Metadata  string   // JSON metadata
    IsError   bool
}
```

Constructors:
- `NewTextResponse(content string) ToolResponse`
- `NewTextErrorResponse(content string) ToolResponse`
- `NewImageResponse(data []byte, mediaType string) ToolResponse`
- `NewMediaResponse(data []byte, mediaType string) ToolResponse`
- `WithResponseMetadata(response, metadata any) ToolResponse`

### Creating Tools

```go
// Typed tool with automatic schema generation from struct
func NewAgentTool[TInput any](
    name string,
    description string,
    fn func(ctx context.Context, input TInput, call ToolCall) (ToolResponse, error),
) AgentTool

// Same but marked safe for parallel execution
func NewParallelAgentTool[TInput any](
    name, description string,
    fn func(ctx context.Context, input TInput, call ToolCall) (ToolResponse, error),
) AgentTool
```

Schema automatically generated from the `TInput` struct type using `schema.Generate(reflect.TypeOf(input))`.

### FunctionTool (Low-Level)

```go
type FunctionTool struct {
    Name            string
    Description     string
    InputSchema     map[string]any   // JSON Schema
    ProviderOptions ProviderOptions
}
// GetType() -> ToolTypeFunction
```

### ProviderDefinedTool

```go
type ProviderDefinedTool struct {
    ID   string          // "<provider-name>.<unique-tool-name>"
    Name string
    Args map[string]any
}
// GetType() -> ToolTypeProviderDefined
```

### ExecutableProviderTool

Provider-defined tool with client-side execution:

```go
type ExecutableProviderTool struct {
    pdt ProviderDefinedTool
    run func(ctx context.Context, call ToolCall) (ToolResponse, error)
}

func NewExecutableProviderTool(
    pdt ProviderDefinedTool,
    run func(ctx context.Context, call ToolCall) (ToolResponse, error),
) ExecutableProviderTool
```

### ToolResultOutputContent

```go
type ToolResultOutputContent interface {
    GetType() ToolResultContentType
}

type ToolResultContentType string
const (
    ToolResultContentTypeText  = "text"
    ToolResultContentTypeError = "error"
    ToolResultContentTypeMedia = "media"
)

type ToolResultOutputContentText struct {
    Text string
}

type ToolResultOutputContentError struct {
    Error error
}

type ToolResultOutputContentMedia struct {
    Data      string  // base64
    MediaType string
    Text      string  // optional
}
```

### Tool Interface (Wire Format)

```go
type ToolType string
const (
    ToolTypeFunction        ToolType = "function"
    ToolTypeProviderDefined ToolType = "provider-defined"
)

type Tool interface {
    GetType() ToolType
    GetName() string
}
```

## Call / Response Types

### Call

```go
type Call struct {
    Prompt           Prompt       // []Message
    MaxOutputTokens  *int64
    Temperature      *float64
    TopP             *float64
    TopK             *int64
    PresencePenalty  *float64
    FrequencyPenalty *float64
    Tools            []Tool
    ToolChoice       *ToolChoice
    UserAgent        string
    ProviderOptions  ProviderOptions
}
```

### ToolChoice

```go
type ToolChoice string
const (
    ToolChoiceNone     ToolChoice = "none"
    ToolChoiceAuto     ToolChoice = "auto"
    ToolChoiceRequired ToolChoice = "required"
)
func SpecificToolChoice(name string) ToolChoice
```

### Response

```go
type Response struct {
    Content          ResponseContent
    FinishReason     FinishReason
    Usage            Usage
    Warnings         []CallWarning
    ProviderMetadata ProviderMetadata
}
```

### Usage

```go
type Usage struct {
    InputTokens         int64
    OutputTokens        int64
    TotalTokens         int64
    ReasoningTokens     int64
    CacheCreationTokens int64
    CacheReadTokens     int64
}
```

### FinishReason

```go
const (
    FinishReasonStop          FinishReason = "stop"
    FinishReasonLength        FinishReason = "length"
    FinishReasonContentFilter FinishReason = "content-filter"
    FinishReasonToolCalls     FinishReason = "tool-calls"
    FinishReasonError         FinishReason = "error"
    FinishReasonOther         FinishReason = "other"
    FinishReasonUnknown       FinishReason = "unknown"
)
```

### CallWarning

```go
type CallWarning struct {
    Type    CallWarningType  // "unsupported-setting", "unsupported-tool", "other"
    Setting string
    Tool    Tool
    Details string
    Message string
}
```

## StreamPart

```go
type StreamPartType string
const (
    StreamPartTypeWarnings       = "warnings"
    StreamPartTypeTextStart      = "text_start"
    StreamPartTypeTextDelta      = "text_delta"
    StreamPartTypeTextEnd        = "text_end"
    StreamPartTypeReasoningStart = "reasoning_start"
    StreamPartTypeReasoningDelta = "reasoning_delta"
    StreamPartTypeReasoningEnd   = "reasoning_end"
    StreamPartTypeToolInputStart = "tool_input_start"
    StreamPartTypeToolInputDelta = "tool_input_delta"
    StreamPartTypeToolInputEnd   = "tool_input_end"
    StreamPartTypeToolCall       = "tool_call"
    StreamPartTypeToolResult     = "tool_result"
    StreamPartTypeSource         = "source"
    StreamPartTypeFinish         = "finish"
    StreamPartTypeError          = "error"
)

type StreamPart struct {
    Type             StreamPartType
    ID               string
    ToolCallName     string
    ToolCallInput    string
    Delta            string
    ProviderExecuted bool
    Usage            Usage
    FinishReason     FinishReason
    Error            error
    Warnings         []CallWarning
    SourceType       SourceType
    URL              string
    Title            string
    ProviderMetadata ProviderMetadata
}

type StreamResponse = iter.Seq[StreamPart]
```

## AgentCall / AgentStreamCall

### AgentCall

```go
type AgentCall struct {
    Prompt           string
    Files            []FilePart
    Messages         []Message
    MaxOutputTokens  *int64
    Temperature      *float64
    TopP, TopK       *float64, *int64
    PresencePenalty  *float64
    FrequencyPenalty *float64
    ActiveTools      []string
    ProviderOptions  ProviderOptions
    OnRetry          OnRetryCallback
    MaxRetries       *int
    StopWhen         []StopCondition
    PrepareStep      PrepareStepFunction
    RepairToolCall   RepairToolCallFunction
}
```

### AgentStreamCall

Extends AgentCall with all streaming callbacks:

```go
type AgentStreamCall struct {
    // Same fields as AgentCall, plus:
    Headers map[string]string

    // Agent-level callbacks
    OnAgentStart  OnAgentStartFunc
    OnAgentFinish OnAgentFinishFunc
    OnStepStart   OnStepStartFunc
    OnStepFinish  OnStepFinishFunc
    OnFinish      OnFinishFunc
    OnError       OnErrorFunc

    // Stream part callbacks
    OnChunk          OnChunkFunc
    OnWarnings       OnWarningsFunc
    OnTextStart      OnTextStartFunc
    OnTextDelta      OnTextDeltaFunc
    OnTextEnd        OnTextEndFunc
    OnReasoningStart OnReasoningStartFunc
    OnReasoningDelta OnReasoningDeltaFunc
    OnReasoningEnd   OnReasoningEndFunc
    OnToolInputStart OnToolInputStartFunc
    OnToolInputDelta OnToolInputDeltaFunc
    OnToolInputEnd   OnToolInputEndFunc
    OnToolCall       OnToolCallFunc
    OnToolResult     OnToolResultFunc
    OnSource         OnSourceFunc
    OnStreamFinish   OnStreamFinishFunc
}
```

### Callback Types

**Agent-level:**
```go
type OnAgentStartFunc  func()
type OnAgentFinishFunc func(result *AgentResult) error
type OnStepStartFunc   func(stepNumber int) error
type OnStepFinishFunc  func(stepResult StepResult) error
type OnFinishFunc      func(result *AgentResult)
type OnErrorFunc       func(error)
```

**Stream part:**
```go
type OnChunkFunc          func(StreamPart) error
type OnWarningsFunc       func(warnings []CallWarning) error
type OnTextStartFunc      func(id string) error
type OnTextDeltaFunc      func(id, text string) error
type OnTextEndFunc        func(id string) error
type OnReasoningStartFunc func(id string, reasoning ReasoningContent) error
type OnReasoningDeltaFunc func(id, text string) error
type OnReasoningEndFunc   func(id string, reasoning ReasoningContent) error
type OnToolInputStartFunc func(id, toolName string) error
type OnToolInputDeltaFunc func(id, delta string) error
type OnToolInputEndFunc   func(id string) error
type OnToolCallFunc       func(toolCall ToolCallContent) error
type OnToolResultFunc     func(result ToolResultContent) error
type OnSourceFunc         func(source SourceContent) error
type OnStreamFinishFunc   func(usage Usage, finishReason FinishReason, providerMetadata ProviderMetadata) error
```

### AgentResult

```go
type AgentResult struct {
    Steps      []StepResult
    Response   Response
    TotalUsage Usage
}

type StepResult struct {
    Response
    Messages []Message
}
```

## Stop Conditions

```go
type StopCondition = func(steps []StepResult) bool

func StepCountIs(stepCount int) StopCondition
func HasToolCall(toolName string) StopCondition
func HasContent(contentType ContentType) StopCondition
func FinishReasonIs(reason FinishReason) StopCondition
func MaxTokensUsed(maxTokens int64) StopCondition
```

## PrepareStep

```go
type PrepareStepFunctionOptions struct {
    Steps      []StepResult
    StepNumber int
    Model      LanguageModel
    Messages   []Message
}

type PrepareStepResult struct {
    Model           LanguageModel
    Messages        []Message
    System          *string
    ToolChoice      *ToolChoice
    ActiveTools     []string
    DisableAllTools bool
    Tools           []AgentTool
}

type PrepareStepFunction = func(ctx context.Context, options PrepareStepFunctionOptions) (context.Context, PrepareStepResult, error)
```

## Agent Options

```go
type AgentOption = func(*agentSettings)

// Available via With* functions:
// WithSystemPrompt(string)
// WithTools(tools ...AgentTool)
// WithMaxOutputTokens(int64)
// WithTemperature(float64)
// WithMaxRetries(int)
// WithProviderOptions(ProviderOptions)
// WithHeaders(map[string]string)
// WithUserAgent(string)
// WithStopWhen(conditions ...StopCondition)
// WithPrepareStep(PrepareStepFunction)
// WithRepairToolCall(RepairToolCallFunction)
// WithProviderDefinedTools(tools ...ProviderDefinedTool)
// WithExecutableProviderTools(tools ...ExecutableProviderTool)
// WithOnRetry(OnRetryCallback)
```

## ObjectCall / ObjectResponse

### ObjectCall

```go
type ObjectCall struct {
    Prompt            Prompt
    Schema            Schema
    SchemaName        string
    SchemaDescription string
    MaxOutputTokens   *int64
    Temperature       *float64
    TopP, TopK        *float64, *int64
    PresencePenalty   *float64
    FrequencyPenalty  *float64
    UserAgent         string
    ProviderOptions   ProviderOptions
    RepairText        schema.ObjectRepairFunc
}
```

### ObjectResponse

```go
type ObjectResponse struct {
    Object           any
    RawText          string
    Usage            Usage
    FinishReason     FinishReason
    Warnings         []CallWarning
    ProviderMetadata ProviderMetadata
}
```

### ObjectMode

```go
const (
    ObjectModeAuto ObjectMode = "auto"
    ObjectModeJSON ObjectMode = "json"
    ObjectModeTool ObjectMode = "tool"
    ObjectModeText ObjectMode = "text"
)
```

### Streaming Objects

```go
type ObjectStreamPart struct {
    Type             ObjectStreamPartType  // "object", "text-delta", "error", "finish"
    Object           any
    Delta            string
    Error            error
    Usage            Usage
    FinishReason     FinishReason
    Warnings         []CallWarning
    ProviderMetadata ProviderMetadata
}

type ObjectStreamResponse = iter.Seq[ObjectStreamPart]

// Typed wrapper
type StreamObjectResult[T any] struct { ... }
func NewStreamObjectResult[T any](ctx, stream) *StreamObjectResult[T]
func (s *StreamObjectResult[T]) PartialObjectStream() iter.Seq[T]
func (s *StreamObjectResult[T]) TextStream() iter.Seq[string]
func (s *StreamObjectResult[T]) FullStream() iter.Seq[ObjectStreamPart]
func (s *StreamObjectResult[T]) Object() (*ObjectResult[T], error)

type ObjectResult[T any] struct {
    Object           T
    RawText          string
    Usage            Usage
    FinishReason     FinishReason
    Warnings         []CallWarning
    ProviderMetadata ProviderMetadata
}
```

## Schema Package

`charm.land/fantasy/schema`

- `Schema` type (JSON Schema representation)
- `Generate(reflect.Type) Schema` -- auto-generates JSON Schema from Go struct types
- `ToParameters(Schema) map[string]any` -- converts to tool parameters format
- `ObjectRepairFunc` type for repairing malformed object generation

## Retry Logic

### RetryOptions

```go
type RetryOptions struct {
    MaxRetries     int
    InitialDelayIn time.Duration
    BackoffFactor  float64
    OnRetry        OnRetryCallback
}

type OnRetryCallback = func(err *ProviderError, delay time.Duration)

func DefaultRetryOptions() RetryOptions  // MaxRetries: 2, InitialDelay: 2s, BackoffFactor: 2.0
```

### Retry Behavior

`RetryWithExponentialBackoffRespectingRetryHeaders[T]` creates a retry function that:
1. Executes the function
2. On error, checks if abort error (context cancelled) -> no retry
3. Checks if `ProviderError.IsRetryable()` -> retry with delay
4. Respects `retry-after-ms` and `retry-after` headers (0-60 second range)
5. Falls back to exponential backoff
6. Accumulates errors in `RetryError.Errors`

### IsRetryable

`ProviderError.IsRetryable()` returns true for:
- `io.ErrUnexpectedEOF`
- `x-should-retry: true` header
- Status codes: 408 (timeout), 409 (conflict), 429 (rate limit), 5xx (server errors)

## ProviderOptions / ProviderMetadata

```go
// Provider-specific options passed TO the provider
type ProviderOptions map[string]ProviderOptionsData

// Provider-specific metadata returned FROM the provider
type ProviderMetadata map[string]ProviderOptionsData

type ProviderOptionsData interface {
    Options()  // Marker method
    json.Marshaler
    json.Unmarshaler
}
```

### Provider Registry

Type-safe serialization via registry:

```go
func RegisterProviderType(typeID string, unmarshalFn UnmarshalFunc)
func MarshalProviderType[T any](typeID string, data T) ([]byte, error)
func UnmarshalProviderType[T any](data []byte, target *T) error
func UnmarshalProviderOptions(data map[string]json.RawMessage) (ProviderOptions, error)
func UnmarshalProviderMetadata(data map[string]json.RawMessage) (ProviderMetadata, error)
```

Serialized as `{"type": "provider.options", "data": {...}}`. Provider types registered in `init()` functions.

## Error Types

### Error

```go
type Error struct {
    Message string
    Title   string
    Cause   error
}
```

### ProviderError

```go
type ProviderError struct {
    Message, Title string
    Cause          error
    URL            string
    StatusCode     int
    RequestBody    []byte
    ResponseHeaders map[string]string
    ResponseBody   []byte
    ContextUsedTokens  int
    ContextMaxTokens   int
    ContextTooLargeErr bool
}
```

Methods:
- `IsRetryable() bool` -- checks headers and status codes
- `IsContextTooLarge() bool` -- checks context token limits

### RetryError

```go
type RetryError struct {
    Errors []error  // All errors from retries
}
```

`Unwrap()` returns the last error.

### NoObjectGeneratedError

```go
type NoObjectGeneratedError struct {
    RawText         string
    ParseError      error
    ValidationError error
    Usage           Usage
    FinishReason    FinishReason
}

func IsNoObjectGeneratedError(err error) bool
```

## Provider Implementations

Located in `providers/` directory:

| Package | Provider | Description |
|---------|----------|-------------|
| `anthropic` | Anthropic | Claude models (Messages API) |
| `openai` | OpenAI | GPT models + Responses API |
| `google` | Google | Gemini models |
| `azure` | Azure | Azure OpenAI |
| `bedrock` | Bedrock | AWS Bedrock |
| `openrouter` | OpenRouter | Multi-provider routing |
| `openaicompat` | OpenAI-compatible | Generic OpenAI-compatible APIs |
| `vercel` | Vercel | Vercel AI gateway |
| `kronk` | Kronk | Internal provider |

Plus `internal/` package for shared provider utilities.

## Utility Functions

```go
// Create an optional pointer
func Opt[T any](v T) *T

// Parse options map into struct using mapstructure
func ParseOptions[T any](options map[string]any, m *T) error
```
