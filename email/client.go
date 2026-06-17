package email

import (
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// IMAPClient wraps IMAP connection and operations
type IMAPClient struct {
	Host     string
	Port     int
	Username string
	Password string
	client   *imapclient.Client
}

// SMTPClient wraps SMTP connection and operations
type SMTPClient struct {
	Host     string
	Port     int
	Username string
	Password string
}

// EmailMessage represents an email message
type EmailMessage struct {
	UID              imap.UID     `json:"uid"`
	MessageID        string       `json:"messageId"`
	ThreadID         string       `json:"threadId"`
	Subject          string       `json:"subject"`
	From             []string     `json:"from"`
	To               []string     `json:"to"`
	CC               []string     `json:"cc"`
	BCC              []string     `json:"bcc"`
	ReplyTo          []string     `json:"replyTo"`
	Date             time.Time    `json:"date"`
	Body             string       `json:"body"`
	BodyHTML         string       `json:"bodyHtml"`
	Snippet          string       `json:"snippet"`
	IsRead           bool         `json:"isRead"`
	HasAttachment    bool         `json:"hasAttachment"`
	Attachments      []Attachment `json:"attachments,omitempty"`
	InReplyTo        string       `json:"inReplyTo,omitempty"`
	References       []string     `json:"references,omitempty"`
	Flags            []string     `json:"flags"`
	Size             int64        `json:"size"`
	Mailbox          string       `json:"mailbox"`
	ThreadCount      int          `json:"threadCount,omitempty"`      // Number of messages in this thread
	ThreadMessageIds []string     `json:"threadMessageIds,omitempty"` // All message IDs in this thread
}

// Attachment represents an email attachment
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	Data        []byte `json:"data,omitempty"`
}

// EmailThread represents a thread of emails
type EmailThread struct {
	ThreadID string         `json:"threadId"`
	Subject  string         `json:"subject"`
	Messages []EmailMessage `json:"messages"`
	LastDate time.Time      `json:"lastDate"`
	IsRead   bool           `json:"isRead"`
	Snippet  string         `json:"snippet"`
}

// NewIMAPClient creates a new IMAP client
func NewIMAPClient(host string, port int, username, password string) *IMAPClient {
	return &IMAPClient{
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
	}
}

// Connect establishes connection to IMAP server
func (c *IMAPClient) Connect() error {
	var err error
	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)

	// Use TLS connection
	tlsConfig := &tls.Config{
		ServerName: c.Host,
	}

	options := &imapclient.Options{
		TLSConfig: tlsConfig,
	}

	c.client, err = imapclient.DialTLS(addr, options)
	if err != nil {
		return fmt.Errorf("failed to connect to IMAP server: %w", err)
	}

	// Login
	if err := c.client.Login(c.Username, c.Password).Wait(); err != nil {
		c.client.Close()
		return fmt.Errorf("failed to login: %w", err)
	}

	return nil
}

// Disconnect closes the IMAP connection
func (c *IMAPClient) Disconnect() error {
	if c.client != nil {
		return c.client.Logout().Wait()
	}
	return nil
}

// FetchEmails retrieves emails from a mailbox
func (c *IMAPClient) FetchEmails(mailbox string, limit int) ([]EmailMessage, error) {
	if c.client == nil {
		return nil, fmt.Errorf("not connected to IMAP server")
	}

	// Select mailbox
	selectCmd := c.client.Select(mailbox, nil)
	mbox, err := selectCmd.Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to select mailbox: %w", err)
	}

	if mbox.NumMessages == 0 {
		return []EmailMessage{}, nil
	}

	// Calculate sequence set
	start := uint32(1)
	end := mbox.NumMessages
	if limit > 0 && int(mbox.NumMessages) > limit {
		start = mbox.NumMessages - uint32(limit) + 1
	}

	seqSet := imap.SeqSet{}
	seqSet.AddRange(start, end)

	// Fetch messages
	fetchOptions := &imap.FetchOptions{
		Envelope:    true,
		Flags:       true,
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{{}},
		RFC822Size:  true,
	}

	messages := []EmailMessage{}
	fetchCmd := c.client.Fetch(seqSet, fetchOptions)

	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break
		}

		emailMsg, err := c.parseMessage(msg, mailbox)
		if err != nil {
			continue // Skip messages that fail to parse
		}
		messages = append(messages, emailMsg)
	}

	if err := fetchCmd.Close(); err != nil {
		return messages, fmt.Errorf("fetch error: %w", err)
	}

	return messages, nil
}

// parseMessage parses an IMAP message into EmailMessage
func (c *IMAPClient) parseMessage(msg *imapclient.FetchMessageData, mailbox string) (EmailMessage, error) {
	// Collect message data into buffer
	buf, err := msg.Collect()
	if err != nil {
		return EmailMessage{}, fmt.Errorf("failed to collect message data: %w", err)
	}

	emailMsg := EmailMessage{
		UID:     buf.UID,
		Mailbox: mailbox,
		Size:    buf.RFC822Size,
	}

	// Parse flags
	if buf.Flags != nil {
		for _, flag := range buf.Flags {
			emailMsg.Flags = append(emailMsg.Flags, string(flag))
			if flag == imap.FlagSeen {
				emailMsg.IsRead = true
			}
		}
	}

	// Parse envelope
	if buf.Envelope != nil {
		emailMsg.Subject = buf.Envelope.Subject
		emailMsg.Date = buf.Envelope.Date
		// Normalize MessageID by removing angle brackets
		emailMsg.MessageID = strings.Trim(buf.Envelope.MessageID, "<>")
		if len(buf.Envelope.InReplyTo) > 0 {
			// Normalize InReplyTo by removing angle brackets
			emailMsg.InReplyTo = strings.Trim(buf.Envelope.InReplyTo[0], "<>")
		}

		if len(buf.Envelope.From) > 0 {
			for _, addr := range buf.Envelope.From {
				emailMsg.From = append(emailMsg.From, formatAddress(addr))
			}
		}

		if len(buf.Envelope.To) > 0 {
			for _, addr := range buf.Envelope.To {
				emailMsg.To = append(emailMsg.To, formatAddress(addr))
			}
		}

		if len(buf.Envelope.Cc) > 0 {
			for _, addr := range buf.Envelope.Cc {
				emailMsg.CC = append(emailMsg.CC, formatAddress(addr))
			}
		}

		if len(buf.Envelope.ReplyTo) > 0 {
			for _, addr := range buf.Envelope.ReplyTo {
				emailMsg.ReplyTo = append(emailMsg.ReplyTo, formatAddress(addr))
			}
		}
	}

	// Parse body
	for section, literal := range buf.BodySection {
		if section.Specifier == imap.PartSpecifierNone {
			bodyBytes := literal

			// Parse the full message
			mailMsg, err := mail.ReadMessage(strings.NewReader(string(bodyBytes)))
			if err != nil {
				continue
			}

			// Extract body content
			body, htmlBody, attachments := parseMessageBody(mailMsg)
			emailMsg.Body = body
			emailMsg.BodyHTML = htmlBody
			emailMsg.Attachments = attachments
			emailMsg.HasAttachment = len(attachments) > 0

			// Extract In-Reply-To from headers (as backup if envelope didn't have it)
			if emailMsg.InReplyTo == "" {
				if inReplyToHeader := mailMsg.Header.Get("In-Reply-To"); inReplyToHeader != "" {
					emailMsg.InReplyTo = strings.Trim(strings.TrimSpace(inReplyToHeader), "<>")
				}
			}

			// Also extract Message-ID from headers if not set (normalize it)
			if emailMsg.MessageID == "" {
				if msgIDHeader := mailMsg.Header.Get("Message-ID"); msgIDHeader != "" {
					emailMsg.MessageID = strings.Trim(strings.TrimSpace(msgIDHeader), "<>")
				}
			}

			// Extract References header
			if referencesHeader := mailMsg.Header.Get("References"); referencesHeader != "" {
				// References can be space or comma separated
				refs := strings.FieldsFunc(referencesHeader, func(r rune) bool {
					return r == ' ' || r == ',' || r == '\n' || r == '\r' || r == '\t'
				})
				for _, ref := range refs {
					ref = strings.TrimSpace(ref)
					// Clean up angle brackets if present
					ref = strings.Trim(ref, "<>")
					if ref != "" {
						emailMsg.References = append(emailMsg.References, ref)
					}
				}
			}

			// Create snippet (first 150 chars of text body)
			snippet := body
			if len(snippet) > 150 {
				snippet = snippet[:150] + "..."
			}
			emailMsg.Snippet = snippet
		}
	}

	// Set initial ThreadID - will be properly grouped later
	// Use the root message ID from References, or InReplyTo, or self
	if len(emailMsg.References) > 0 {
		// First reference is usually the root of the thread
		emailMsg.ThreadID = emailMsg.References[0]
	} else if emailMsg.InReplyTo != "" {
		emailMsg.ThreadID = emailMsg.InReplyTo
	} else {
		emailMsg.ThreadID = emailMsg.MessageID
	}

	return emailMsg, nil
}

// formatAddress formats an IMAP address
func formatAddress(addr imap.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s@%s>", addr.Name, addr.Mailbox, addr.Host)
	}
	return fmt.Sprintf("%s@%s", addr.Mailbox, addr.Host)
}

// parseMessageBody extracts text and HTML body from a message
func parseMessageBody(msg *mail.Message) (textBody, htmlBody string, attachments []Attachment) {
	contentType := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// Try to read as plain text
		body, _ := io.ReadAll(msg.Body)
		return string(body), "", nil
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		mr := multipart.NewReader(msg.Body, boundary)

		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}

			partContentType := part.Header.Get("Content-Type")
			partMediaType, _, _ := mime.ParseMediaType(partContentType)

			disposition := part.Header.Get("Content-Disposition")
			isAttachment := strings.HasPrefix(disposition, "attachment")

			partBody, err := io.ReadAll(part)
			if err != nil {
				continue
			}

			if isAttachment {
				_, params, _ := mime.ParseMediaType(disposition)
				filename := params["filename"]
				if filename == "" {
					filename = "attachment"
				}

				attachments = append(attachments, Attachment{
					Filename:    filename,
					ContentType: partMediaType,
					Size:        int64(len(partBody)),
					Data:        partBody,
				})
			} else {
				switch partMediaType {
				case "text/plain":
					if textBody == "" {
						textBody = string(partBody)
					}
				case "text/html":
					if htmlBody == "" {
						htmlBody = string(partBody)
					}
				}
			}
		}
	} else if mediaType == "text/plain" {
		body, _ := io.ReadAll(msg.Body)
		textBody = string(body)
	} else if mediaType == "text/html" {
		body, _ := io.ReadAll(msg.Body)
		htmlBody = string(body)
	}

	return textBody, htmlBody, attachments
}

// SearchEmails searches for emails matching a query
func (c *IMAPClient) SearchEmails(mailbox string, query string) ([]EmailMessage, error) {
	if c.client == nil {
		return nil, fmt.Errorf("not connected to IMAP server")
	}

	// Select mailbox
	_, err := c.client.Select(mailbox, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to select mailbox: %w", err)
	}

	// Build search criteria
	criteria := &imap.SearchCriteria{
		Text: []string{query},
	}

	// Perform search
	searchCmd := c.client.Search(criteria, nil)
	searchData, err := searchCmd.Wait()
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	if len(searchData.AllUIDs()) == 0 {
		return []EmailMessage{}, nil
	}

	// Fetch the found messages
	seqSet := imap.UIDSet{}
	for _, uid := range searchData.AllUIDs() {
		seqSet.AddNum(uid)
	}

	fetchOptions := &imap.FetchOptions{
		Envelope:    true,
		Flags:       true,
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{{}},
		RFC822Size:  true,
	}

	messages := []EmailMessage{}
	fetchCmd := c.client.Fetch(seqSet, fetchOptions)

	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break
		}

		emailMsg, err := c.parseMessage(msg, mailbox)
		if err != nil {
			continue
		}
		messages = append(messages, emailMsg)
	}

	if err := fetchCmd.Close(); err != nil {
		return messages, fmt.Errorf("fetch error: %w", err)
	}

	return messages, nil
}

// MarkAsRead marks an email as read
func (c *IMAPClient) MarkAsRead(mailbox string, uid imap.UID) error {
	if c.client == nil {
		return fmt.Errorf("not connected to IMAP server")
	}

	_, err := c.client.Select(mailbox, nil).Wait()
	if err != nil {
		return fmt.Errorf("failed to select mailbox: %w", err)
	}

	seqSet := imap.UIDSet{}
	seqSet.AddNum(uid)

	storeFlags := imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Flags:  []imap.Flag{imap.FlagSeen},
		Silent: false,
	}

	return c.client.Store(seqSet, &storeFlags, nil).Close()
}

// MoveToTrash moves an email to trash
func (c *IMAPClient) MoveToTrash(mailbox string, uid imap.UID) error {
	if c.client == nil {
		return fmt.Errorf("not connected to IMAP server")
	}

	_, err := c.client.Select(mailbox, nil).Wait()
	if err != nil {
		return fmt.Errorf("failed to select mailbox: %w", err)
	}

	seqSet := imap.UIDSet{}
	seqSet.AddNum(uid)

	// Try common trash folder names
	trashFolders := []string{"Trash", "[Gmail]/Trash", "Deleted Items", "Deleted Messages"}

	var moveErr error
	for _, trashFolder := range trashFolders {
		moveCmd := c.client.Move(seqSet, trashFolder)
		_, moveErr = moveCmd.Wait()
		if moveErr == nil {
			return nil
		}
	}

	// If move failed, just mark as deleted
	storeFlags := imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Flags:  []imap.Flag{imap.FlagDeleted},
		Silent: false,
	}

	return c.client.Store(seqSet, &storeFlags, nil).Close()
}

// DeleteEmail permanently deletes an email
func (c *IMAPClient) DeleteEmail(mailbox string, uid imap.UID) error {
	if c.client == nil {
		return fmt.Errorf("not connected to IMAP server")
	}

	_, err := c.client.Select(mailbox, nil).Wait()
	if err != nil {
		return fmt.Errorf("failed to select mailbox: %w", err)
	}

	seqSet := imap.UIDSet{}
	seqSet.AddNum(uid)

	// Mark as deleted
	storeFlags := imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Flags:  []imap.Flag{imap.FlagDeleted},
		Silent: false,
	}

	if err := c.client.Store(seqSet, &storeFlags, nil).Close(); err != nil {
		return err
	}

	// Expunge to permanently delete
	return c.client.Expunge().Close()
}

// SendEmail sends an email via SMTP
func (s *SMTPClient) SendEmail(from string, to []string, subject string, body string, htmlBody string, replyTo string, inReplyTo string, references []string) error {
	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	// Build email headers and body
	message := buildEmailMessage(from, to, subject, body, htmlBody, replyTo, inReplyTo, references)

	// Setup authentication using standard SMTP auth
	auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)

	// Connect with TLS
	tlsConfig := &tls.Config{
		ServerName: s.Host,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	// Auth
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Set sender
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}

	// Set recipients
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("failed to set recipient %s: %w", recipient, err)
		}
	}

	// Send message
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to open data writer: %w", err)
	}

	_, err = w.Write([]byte(message))
	if err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close data writer: %w", err)
	}

	return client.Quit()
}

// buildEmailMessage builds the raw email message
func buildEmailMessage(from string, to []string, subject string, body string, htmlBody string, replyTo string, inReplyTo string, references []string) string {
	boundary := fmt.Sprintf("boundary_%d", time.Now().Unix())

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))

	// Extract domain from email for Message-ID
	domain := "localhost"
	if atIndex := strings.Index(from, "@"); atIndex != -1 {
		domain = from[atIndex+1:]
	}
	msg.WriteString(fmt.Sprintf("Message-ID: <%d@%s>\r\n", time.Now().UnixNano(), domain))

	if replyTo != "" {
		msg.WriteString(fmt.Sprintf("Reply-To: %s\r\n", replyTo))
	}

	if inReplyTo != "" {
		msg.WriteString(fmt.Sprintf("In-Reply-To: %s\r\n", inReplyTo))
	}

	if len(references) > 0 {
		msg.WriteString(fmt.Sprintf("References: %s\r\n", strings.Join(references, " ")))
	}

	msg.WriteString("MIME-Version: 1.0\r\n")

	if htmlBody != "" {
		msg.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
		msg.WriteString("\r\n")

		// Plain text part
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		msg.WriteString("\r\n")
		msg.WriteString(body)
		msg.WriteString("\r\n\r\n")

		// HTML part
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		msg.WriteString("\r\n")
		msg.WriteString(htmlBody)
		msg.WriteString("\r\n\r\n")

		msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else {
		msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		msg.WriteString("\r\n")
		msg.WriteString(body)
	}

	return msg.String()
}

// TestConnection tests IMAP connection
func (c *IMAPClient) TestConnection() error {
	if err := c.Connect(); err != nil {
		return err
	}
	return c.Disconnect()
}

// GetMailboxes lists all mailboxes
func (c *IMAPClient) GetMailboxes() ([]string, error) {
	if c.client == nil {
		return nil, fmt.Errorf("not connected to IMAP server")
	}

	listCmd := c.client.List("", "*", nil)

	mailboxes := []string{}
	for {
		mbox := listCmd.Next()
		if mbox == nil {
			break
		}
		mailboxes = append(mailboxes, mbox.Mailbox)
	}

	if err := listCmd.Close(); err != nil {
		return mailboxes, err
	}

	return mailboxes, nil
}
