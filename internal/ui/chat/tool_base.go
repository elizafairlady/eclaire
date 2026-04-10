package chat

// baseToolItem provides common state for all tool message items.
type baseToolItem struct {
	id       string
	name     string
	input    string
	output   string
	isError  bool
	status   ToolStatus
	expanded bool
	compact  bool
}

func (b *baseToolItem) ID() string       { return b.id }
func (b *baseToolItem) ToolName() string  { return b.name }
func (b *baseToolItem) ToolInput() string { return b.input }
func (b *baseToolItem) Status() ToolStatus { return b.status }
func (b *baseToolItem) IsExpanded() bool  { return b.expanded }
func (b *baseToolItem) IsCompact() bool   { return b.compact }

func (b *baseToolItem) SetStatus(s ToolStatus)  { b.status = s }
func (b *baseToolItem) SetCompact(c bool)        { b.compact = c }
func (b *baseToolItem) ToggleExpanded()           { b.expanded = !b.expanded }

func (b *baseToolItem) SetResult(output string, isError bool) {
	b.output = output
	b.isError = isError
	if isError {
		b.status = ToolError
	} else {
		b.status = ToolSuccess
	}
}
