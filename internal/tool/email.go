package tool

import (
	"context"
	"fmt"
	"mime"
	"net/smtp"
	"os"
	"sort"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"gopkg.in/yaml.v3"
)

// EmailCredentials holds IMAP/SMTP connection details.
type EmailCredentials struct {
	IMAPHost string `yaml:"imap_host"` // e.g. "imap.gmail.com"
	IMAPPort int    `yaml:"imap_port"` // e.g. 993
	SMTPHost string `yaml:"smtp_host"` // e.g. "smtp.gmail.com"
	SMTPPort int    `yaml:"smtp_port"` // e.g. 587
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	From     string `yaml:"from"` // defaults to User if empty
}

// LoadEmailCredentials reads credentials from a YAML file.
func LoadEmailCredentials(path string) (*EmailCredentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var creds EmailCredentials
	if err := yaml.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	if creds.From == "" {
		creds.From = creds.User
	}
	if creds.IMAPPort == 0 {
		creds.IMAPPort = 993
	}
	if creds.SMTPPort == 0 {
		creds.SMTPPort = 587
	}
	return &creds, nil
}

// EmailMessage is a simplified email for display.
type EmailMessage struct {
	UID     uint32    `json:"uid"`
	From    string    `json:"from"`
	To      string    `json:"to,omitempty"`
	Subject string    `json:"subject"`
	Date    time.Time `json:"date"`
	Snippet string    `json:"snippet,omitempty"` // first ~200 chars of body
	Flags   []string  `json:"flags,omitempty"`
}

// FormatMessage renders an email for display.
func FormatMessage(m EmailMessage) string {
	flags := ""
	if len(m.Flags) > 0 {
		flags = " [" + strings.Join(m.Flags, ", ") + "]"
	}
	return fmt.Sprintf("UID:%d | %s | From: %s | %s%s\n  %s",
		m.UID, m.Date.Format("2006-01-02 15:04"), m.From, m.Subject, flags, m.Snippet)
}

// EmailDeps holds what the email tool needs.
type EmailDeps struct {
	CredentialsPath string // ~/.eclaire/credentials/email.yaml
}

type emailInput struct {
	Operation string `json:"operation" jsonschema:"description=Operation: inbox read search send"`
	// inbox
	MaxItems int `json:"max_items,omitempty" jsonschema:"description=Max messages to return (default 20)"`
	// read
	UID uint32 `json:"uid,omitempty" jsonschema:"description=Message UID to read"`
	// search
	From    string `json:"from,omitempty" jsonschema:"description=Search by sender"`
	Subject string `json:"subject,omitempty" jsonschema:"description=Search by subject"`
	Since   string `json:"since,omitempty" jsonschema:"description=Search since date (YYYY-MM-DD)"`
	// send
	To   string `json:"to,omitempty" jsonschema:"description=Recipient email address"`
	Body string `json:"body,omitempty" jsonschema:"description=Email body text"`
}

// EmailTool creates the eclaire_email tool.
func EmailTool(deps EmailDeps) Tool {
	return NewTool("eclaire_email",
		"Read and send email via IMAP/SMTP. Operations: inbox (list recent), read (full message by UID), search (by from/subject/since), send (to/subject/body). Requires credentials at ~/.eclaire/credentials/email.yaml.",
		TrustDangerous, "email",
		func(ctx context.Context, input emailInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			creds, err := LoadEmailCredentials(deps.CredentialsPath)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("load credentials: %v (create ~/.eclaire/credentials/email.yaml)", err)), nil
			}

			switch input.Operation {
			case "inbox":
				return handleEmailInbox(ctx, creds, input)
			case "read":
				return handleEmailRead(ctx, creds, input)
			case "search":
				return handleEmailSearch(ctx, creds, input)
			case "send":
				return handleEmailSend(creds, input)
			default:
				return fantasy.NewTextErrorResponse(
					fmt.Sprintf("unknown operation %q; valid: inbox, read, search, send", input.Operation),
				), nil
			}
		},
	)
}

func connectIMAP(ctx context.Context, creds *EmailCredentials) (*imapclient.Client, error) {
	addr := fmt.Sprintf("%s:%d", creds.IMAPHost, creds.IMAPPort)
	opts := &imapclient.Options{
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
	}

	c, err := imapclient.DialTLS(addr, opts)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", addr, err)
	}

	if err := c.Login(creds.User, creds.Password).Wait(); err != nil {
		c.Close()
		return nil, fmt.Errorf("login: %w", err)
	}

	return c, nil
}

func handleEmailInbox(ctx context.Context, creds *EmailCredentials, input emailInput) (fantasy.ToolResponse, error) {
	c, err := connectIMAP(ctx, creds)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	defer c.Logout().Wait()

	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("select INBOX: %v", err)), nil
	}

	maxItems := input.MaxItems
	if maxItems <= 0 {
		maxItems = 20
	}

	// Fetch recent N messages using sequence numbers
	mbox := c.Mailbox()
	if mbox == nil || mbox.NumMessages == 0 {
		return fantasy.ToolResponse{Content: "Inbox is empty."}, nil
	}

	start := uint32(1)
	if mbox.NumMessages > uint32(maxItems) {
		start = mbox.NumMessages - uint32(maxItems) + 1
	}
	var seqSet imap.SeqSet
	seqSet.AddRange(start, mbox.NumMessages)

	fetchOpts := &imap.FetchOptions{
		UID:      true,
		Flags:    true,
		Envelope: true,
	}

	msgs, err := c.Fetch(seqSet, fetchOpts).Collect()
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("fetch: %v", err)), nil
	}

	// Convert and sort by date descending
	var emails []EmailMessage
	for _, msg := range msgs {
		em := bufferToMessage(msg)
		emails = append(emails, em)
	}
	sort.Slice(emails, func(i, j int) bool {
		return emails[i].Date.After(emails[j].Date)
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Inbox: %d messages (showing %d)\n\n", mbox.NumMessages, len(emails)))
	for _, em := range emails {
		sb.WriteString(FormatMessage(em) + "\n\n")
	}
	return fantasy.ToolResponse{Content: sb.String()}, nil
}

func handleEmailRead(ctx context.Context, creds *EmailCredentials, input emailInput) (fantasy.ToolResponse, error) {
	if input.UID == 0 {
		return fantasy.NewTextErrorResponse("uid is required"), nil
	}

	c, err := connectIMAP(ctx, creds)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	defer c.Logout().Wait()

	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("select INBOX: %v", err)), nil
	}

	uidSet := imap.UIDSet{}
	uidSet.AddNum(imap.UID(input.UID))

	bodySection := &imap.FetchItemBodySection{}
	fetchOpts := &imap.FetchOptions{
		UID:         true,
		Envelope:    true,
		Flags:       true,
		BodySection: []*imap.FetchItemBodySection{bodySection},
	}

	msgs, err := c.Fetch(uidSet, fetchOpts).Collect()
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("fetch: %v", err)), nil
	}
	if len(msgs) == 0 {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("message UID %d not found", input.UID)), nil
	}

	buf := msgs[0]
	em := bufferToMessage(buf)
	var body string
	if len(buf.BodySection) > 0 {
		body = string(buf.BodySection[0].Bytes)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("From: %s\nTo: %s\nSubject: %s\nDate: %s\nUID: %d\n\n",
		em.From, em.To, em.Subject, em.Date.Format("2006-01-02 15:04"), em.UID))
	sb.WriteString(body)

	return fantasy.ToolResponse{Content: sb.String()}, nil
}

func handleEmailSearch(ctx context.Context, creds *EmailCredentials, input emailInput) (fantasy.ToolResponse, error) {
	c, err := connectIMAP(ctx, creds)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	defer c.Logout().Wait()

	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("select INBOX: %v", err)), nil
	}

	criteria := &imap.SearchCriteria{}
	if input.From != "" {
		criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{Key: "FROM", Value: input.From})
	}
	if input.Subject != "" {
		criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{Key: "SUBJECT", Value: input.Subject})
	}
	if input.Since != "" {
		t, err := time.Parse("2006-01-02", input.Since)
		if err == nil {
			criteria.Since = t
		}
	}

	searchData, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("search: %v", err)), nil
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return fantasy.ToolResponse{Content: "No messages match the search criteria."}, nil
	}

	// Limit to 50 results
	if len(uids) > 50 {
		uids = uids[len(uids)-50:]
	}

	uidSet := imap.UIDSet{}
	for _, uid := range uids {
		uidSet.AddNum(uid)
	}

	fetchOpts := &imap.FetchOptions{
		UID:      true,
		Flags:    true,
		Envelope: true,
	}

	msgs, err := c.Fetch(uidSet, fetchOpts).Collect()
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("fetch search results: %v", err)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results: %d messages\n\n", len(msgs)))
	for _, msg := range msgs {
		em := bufferToMessage(msg)
		sb.WriteString(FormatMessage(em) + "\n\n")
	}
	return fantasy.ToolResponse{Content: sb.String()}, nil
}

func handleEmailSend(creds *EmailCredentials, input emailInput) (fantasy.ToolResponse, error) {
	if input.To == "" {
		return fantasy.NewTextErrorResponse("to is required"), nil
	}
	if input.Subject == "" {
		return fantasy.NewTextErrorResponse("subject is required"), nil
	}
	if input.Body == "" {
		return fantasy.NewTextErrorResponse("body is required"), nil
	}

	from := creds.From
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		from, input.To, input.Subject, time.Now().Format(time.RFC1123Z), input.Body)

	addr := fmt.Sprintf("%s:%d", creds.SMTPHost, creds.SMTPPort)
	auth := smtp.PlainAuth("", creds.User, creds.Password, creds.SMTPHost)

	if err := smtp.SendMail(addr, auth, from, []string{input.To}, []byte(msg)); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("send: %v", err)), nil
	}

	return fantasy.ToolResponse{
		Content: fmt.Sprintf("Email sent to %s: %s", input.To, input.Subject),
	}, nil
}

func bufferToMessage(buf *imapclient.FetchMessageBuffer) EmailMessage {
	em := EmailMessage{
		UID: uint32(buf.UID),
	}
	for _, f := range buf.Flags {
		em.Flags = append(em.Flags, string(f))
	}
	if buf.Envelope != nil {
		em.Subject = buf.Envelope.Subject
		em.Date = buf.Envelope.Date
		if len(buf.Envelope.From) > 0 {
			em.From = formatAddress(buf.Envelope.From[0])
		}
		if len(buf.Envelope.To) > 0 {
			em.To = formatAddress(buf.Envelope.To[0])
		}
	}
	// Extract snippet from first body section if available
	if len(buf.BodySection) > 0 {
		snippet := string(buf.BodySection[0].Bytes)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		em.Snippet = strings.ReplaceAll(snippet, "\n", " ")
	}
	return em
}

func formatAddress(addr imap.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s@%s>", addr.Name, addr.Mailbox, addr.Host)
	}
	return fmt.Sprintf("%s@%s", addr.Mailbox, addr.Host)
}
