package mailapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

const GmailScope = "https://www.googleapis.com/auth/gmail.modify"

type APIError struct{ Status int }

func (e *APIError) Error() string {
	switch e.Status {
	case 401:
		return "Gmail sign-in expired. Reconnect your account using the Mac setup tool"
	case 403:
		return "Gmail access was denied. Check your account permissions"
	case 404:
		return "This conversation is no longer available in Gmail"
	case 429:
		return "Gmail is busy. Wait a moment and try again"
	default:
		return "Gmail could not complete the request. Please try again"
	}
}

type Client struct {
	Store         *Store
	HTTP          *http.Client
	BaseURL       string
	TokenURL      string
	ExpectedEmail string
	mu            sync.Mutex
}

func NewClient(s *Store) *Client {
	return &Client{Store: s, HTTP: &http.Client{Timeout: 25 * time.Second}, BaseURL: "https://gmail.googleapis.com/gmail/v1/users/me", TokenURL: "https://oauth2.googleapis.com/token"}
}

func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var cr Credentials
	if c.Store.Read("credentials.json", &cr) != nil || cr.RefreshToken == "" || cr.ClientID == "" {
		return "", fmt.Errorf("Connect Gmail using the setup tool on your Mac")
	}
	if c.ExpectedEmail != "" && !strings.EqualFold(c.ExpectedEmail, cr.Email) {
		return "", fmt.Errorf("The Gmail account changed. Close and reopen Mail")
	}
	if cr.AccessToken != "" && cr.Expiry > time.Now().Unix()+60 {
		return cr.AccessToken, nil
	}
	form := url.Values{"client_id": {cr.ClientID}, "client_secret": {cr.ClientSecret}, "refresh_token": {cr.RefreshToken}, "grant_type": {"refresh_token"}}
	req, err := http.NewRequestWithContext(ctx, "POST", c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("Cannot reach Google. Check Wi-Fi and try again")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("Gmail sign-in needs attention. Reconnect using the Mac setup tool")
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		Expires     int64  `json:"expires_in"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&tok) != nil || tok.AccessToken == "" {
		return "", fmt.Errorf("Google returned an invalid sign-in response")
	}
	cr.AccessToken = tok.AccessToken
	cr.Expiry = time.Now().Unix() + tok.Expires
	var current Credentials
	if c.Store.Read("credentials.json", &current) != nil || current.RefreshToken != cr.RefreshToken || current.ClientID != cr.ClientID || !strings.EqualFold(current.Email, cr.Email) {
		return "", fmt.Errorf("The Gmail account changed during sign-in. Close and reopen Mail")
	}
	if c.Store.Write("credentials.json", cr) != nil {
		return "", fmt.Errorf("Could not retain your Gmail sign-in")
	}
	return cr.AccessToken, nil
}

// api never retries mutations, particularly messages/send.
func (c *Client) api(ctx context.Context, method, path string, value, out any) (bool, error) {
	token, err := c.token(ctx)
	if err != nil {
		return false, err
	}
	var body io.Reader
	if value != nil {
		b, e := json.Marshal(value)
		if e != nil {
			return false, e
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return true, fmt.Errorf("Cannot reach Gmail. Check Wi-Fi and try again")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if res.StatusCode == 401 {
			c.mu.Lock()
			var cr Credentials
			if c.Store.Read("credentials.json", &cr) == nil {
				cr.Expiry = 0
				cr.AccessToken = ""
				_ = c.Store.Write("credentials.json", cr)
			}
			c.mu.Unlock()
		}
		return res.StatusCode >= 500, &APIError{res.StatusCode}
	}
	if out != nil {
		if json.NewDecoder(io.LimitReader(res.Body, 12*1024*1024)).Decode(out) != nil {
			return true, fmt.Errorf("Gmail returned an unreadable response")
		}
	}
	return false, nil
}

type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type Part struct {
	MimeType string   `json:"mimeType"`
	Filename string   `json:"filename"`
	Headers  []Header `json:"headers"`
	Body     struct {
		Data         string `json:"data"`
		AttachmentID string `json:"attachmentId"`
		Size         int    `json:"size"`
	} `json:"body"`
	Parts []Part `json:"parts"`
}
type GmailMessage struct {
	ID           string   `json:"id"`
	ThreadID     string   `json:"threadId"`
	Snippet      string   `json:"snippet"`
	LabelIDs     []string `json:"labelIds"`
	Payload      Part     `json:"payload"`
	InternalDate string   `json:"internalDate"`
}
type GmailThread struct {
	ID       string         `json:"id"`
	Messages []GmailMessage `json:"messages"`
}

func contains(xs []string, value string) bool {
	for _, v := range xs {
		if v == value {
			return true
		}
	}
	return false
}
func header(p Part, name string) string {
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, name) {
			d := mime.WordDecoder{CharsetReader: func(cs string, r io.Reader) (io.Reader, error) { return charset.NewReaderLabel(cs, r) }}
			v, err := d.DecodeHeader(h.Value)
			if err == nil {
				return v
			}
			return h.Value
		}
	}
	return ""
}

func HTMLText(src string) string {
	z := xhtml.NewTokenizer(strings.NewReader(src))
	var b strings.Builder
	skip := 0
	for {
		t := z.Next()
		if t == xhtml.ErrorToken {
			break
		}
		if t == xhtml.StartTagToken || t == xhtml.EndTagToken || t == xhtml.SelfClosingTagToken {
			name, _ := z.TagName()
			tag := string(name)
			if tag == "script" || tag == "style" || tag == "head" {
				if t == xhtml.StartTagToken {
					skip++
				} else if t == xhtml.EndTagToken && skip > 0 {
					skip--
				}
				continue
			}
			if skip == 0 && contains([]string{"p", "div", "br", "tr", "li", "h1", "h2", "h3", "blockquote"}, tag) {
				b.WriteByte('\n')
			}
		} else if t == xhtml.TextToken && skip == 0 {
			b.Write(z.Text())
		}
	}
	var lines []string
	empty := false
	for _, line := range strings.Split(b.String(), "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if empty {
				continue
			}
			empty = true
		} else {
			empty = false
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func decodePart(p Part) string {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(p.Body.Data, "="))
	if err != nil {
		return ""
	}
	if len(b) > 128*1024 {
		b = b[:128*1024]
	}
	contentType := header(p, "Content-Type")
	if contentType == "" {
		contentType = p.MimeType
	}
	r, err := charset.NewReader(bytes.NewReader(b), contentType)
	if err == nil {
		if decoded, e := io.ReadAll(io.LimitReader(r, 512*1024)); e == nil {
			b = decoded
		}
	}
	return strings.ToValidUTF8(string(b), "�")
}

func bodyText(p Part) (string, []string) {
	attachments := []string{}
	if p.Filename != "" {
		return "", []string{p.Filename}
	}
	if p.MimeType == "text/plain" && p.Body.Data != "" {
		return decodePart(p), attachments
	}
	if p.MimeType == "text/html" && p.Body.Data != "" {
		return HTMLText(decodePart(p)), attachments
	}
	texts := []string{}
	plainFound := false
	for _, child := range p.Parts {
		text, files := bodyText(child)
		attachments = append(attachments, files...)
		if text == "" {
			continue
		}
		if p.MimeType == "multipart/alternative" {
			if child.MimeType == "text/plain" {
				texts = []string{text}
				plainFound = true
			} else if !plainFound && len(texts) == 0 {
				texts = append(texts, text)
			}
		} else {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n\n"), attachments
}

func convertThread(t GmailThread) Conversation {
	c := Conversation{ID: t.ID, Messages: []Message{}, Updated: time.Now().UTC().Format(time.RFC3339)}
	start := 0
	if len(t.Messages) > 50 {
		start = len(t.Messages) - 50
		c.Shortened = true
	}
	for _, gm := range t.Messages[start:] {
		p := gm.Payload
		text, files := bodyText(p)
		if text == "" {
			text = html.UnescapeString(gm.Snippet)
			if text == "" {
				text = "No text content in this message."
			}
		}
		if len([]rune(text)) > 128*1024 {
			text = string([]rune(text)[:128*1024]) + "\n\n[Long message shortened]"
		}
		for i := range files {
			files[i] = shortText(files[i], 256)
		}
		if len(files) > 20 {
			files = files[:20]
			c.Shortened = true
		}
		c.Messages = append(c.Messages, Message{ID: gm.ID, From: shortText(header(p, "From"), 2048), To: shortText(header(p, "To"), 2048), ReplyTo: shortText(header(p, "Reply-To"), 2048), Subject: shortText(header(p, "Subject"), 2048), Date: shortText(header(p, "Date"), 256), Body: text, MessageID: shortText(header(p, "Message-ID"), 2048), References: shortText(header(p, "References"), 8192), Attachments: files})
		c.Unread = c.Unread || contains(gm.LabelIDs, "UNREAD")
		c.Starred = c.Starred || contains(gm.LabelIDs, "STARRED")
	}
	if len(c.Messages) > 0 {
		c.Subject = c.Messages[0].Subject
	}
	if c.Subject == "" {
		c.Subject = "(No subject)"
	}
	return fitConversation(c)
}

func shortText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return strings.ToValidUTF8(s[:limit], "") + "…"
}

// AppLoad uses Unix sequenced packets. Keep each reply comfortably below the
// default Linux socket buffer limit, including JSON escaping and UTF-8.
func fitConversation(c Conversation) Conversation {
	for {
		b, _ := json.Marshal(c)
		if len(b) <= 160*1024 {
			break
		}
		c.Shortened = true
		if len(c.Messages) > 1 {
			c.Messages = c.Messages[1:]
			continue
		}
		if len(c.Messages) == 0 || len(c.Messages[0].Body) < 1024 {
			break
		}
		c.Messages[0].Body = shortText(c.Messages[0].Body, len(c.Messages[0].Body)/2) + "\n\n[Long message shortened]"
	}
	return c
}

func (c *Client) Profile(ctx context.Context) (string, error) {
	var p struct {
		Email string `json:"emailAddress"`
	}
	_, err := c.api(ctx, "GET", "/profile", nil, &p)
	if err == nil {
		var cr Credentials
		if c.Store.Read("credentials.json", &cr) != nil {
			return "", fmt.Errorf("Could not retain account details")
		}
		cr.Email = p.Email
		err = c.Store.Write("credentials.json", cr)
	}
	return p.Email, err
}

func (c *Client) List(ctx context.Context, folder, query, page string) (Listing, error) {
	q := url.Values{"maxResults": {"20"}, "pageToken": {page}}
	switch folder {
	case "inbox":
		q.Set("labelIds", "INBOX")
	case "starred":
		q.Set("labelIds", "STARRED")
	case "sent":
		q.Set("labelIds", "SENT")
	case "all":
	default:
		return Listing{}, fmt.Errorf("Unknown mailbox")
	}
	if query != "" {
		q.Set("q", query)
	}
	var remote struct {
		Threads []struct {
			ID string `json:"id"`
		} `json:"threads"`
		Next string `json:"nextPageToken"`
	}
	if _, err := c.api(ctx, "GET", "/threads?"+q.Encode(), nil, &remote); err != nil {
		return Listing{}, err
	}
	list := Listing{Threads: []Summary{}, Next: remote.Next, Updated: time.Now().UTC().Format(time.RFC3339)}
	// Metadata requests contain only headers and snippets; bodies are fetched on open.
	for _, item := range remote.Threads {
		if !safeID.MatchString(item.ID) {
			return Listing{}, fmt.Errorf("Gmail returned an invalid conversation")
		}
	}
	summaries := make([]*Summary, len(remote.Threads))
	errors := make([]error, len(remote.Threads))
	var workers sync.WaitGroup
	slots := make(chan struct{}, 4)
	for index, item := range remote.Threads {
		workers.Go(func() {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				errors[index] = ctx.Err()
				return
			}
			defer func() { <-slots }()
			var t GmailThread
			if _, err := c.api(ctx, "GET", "/threads/"+item.ID+"?format=metadata&metadataHeaders=From&metadataHeaders=Subject&metadataHeaders=Date", nil, &t); err != nil {
				if apiErr, ok := err.(*APIError); ok && apiErr.Status == 404 {
					return
				}
				errors[index] = err
				return
			}
			if len(t.Messages) == 0 {
				return
			}
			m := t.Messages[len(t.Messages)-1]
			s := Summary{ID: item.ID, Subject: shortText(header(t.Messages[0].Payload, "Subject"), 2048), From: shortText(header(m.Payload, "From"), 2048), Date: shortText(header(m.Payload, "Date"), 256), Snippet: shortText(html.UnescapeString(m.Snippet), 1024), Count: len(t.Messages)}
			if s.Subject == "" {
				s.Subject = "(No subject)"
			}
			for _, m := range t.Messages {
				s.Unread = s.Unread || contains(m.LabelIDs, "UNREAD")
				s.Starred = s.Starred || contains(m.LabelIDs, "STARRED")
			}
			summaries[index] = &s
		})
	}
	workers.Wait()
	for index, s := range summaries {
		if errors[index] != nil {
			return Listing{}, errors[index]
		}
		if s != nil {
			list.Threads = append(list.Threads, *s)
		}
	}
	return list, nil
}

func (c *Client) Thread(ctx context.Context, id string) (Conversation, error) {
	if !safeID.MatchString(id) {
		return Conversation{}, fmt.Errorf("Invalid conversation")
	}
	var t GmailThread
	_, err := c.api(ctx, "GET", "/threads/"+id+"?format=full", nil, &t)
	if err != nil {
		return Conversation{}, err
	}
	return convertThread(t), nil
}

func (c *Client) Modify(ctx context.Context, id, action string) error {
	if !safeID.MatchString(id) {
		return fmt.Errorf("Invalid conversation")
	}
	add, remove := []string{}, []string{}
	switch action {
	case "archive":
		remove = append(remove, "INBOX")
	case "read":
		remove = append(remove, "UNREAD")
	case "unread":
		add = append(add, "UNREAD")
	case "star":
		add = append(add, "STARRED")
	case "unstar":
		remove = append(remove, "STARRED")
	default:
		return fmt.Errorf("Unknown mail action")
	}
	_, err := c.api(ctx, "POST", "/threads/"+id+"/modify", map[string]any{"addLabelIds": add, "removeLabelIds": remove}, nil)
	return err
}

func addressList(raw string, required bool) (string, error) {
	if strings.ContainsAny(raw, "\r\n") {
		return "", fmt.Errorf("Recipient contains a line break")
	}
	if strings.TrimSpace(raw) == "" && !required {
		return "", nil
	}
	list, err := mail.ParseAddressList(raw)
	if err != nil || len(list) == 0 {
		return "", fmt.Errorf("Enter a valid recipient email address")
	}
	result := []string{}
	for _, a := range list {
		if !strings.Contains(a.Address, "@") {
			return "", fmt.Errorf("Enter a full email address")
		}
		result = append(result, a.String())
	}
	return strings.Join(result, ", "), nil
}

func BuildMIME(d Draft, from string) ([]byte, error) {
	to, err := addressList(d.To, true)
	if err != nil {
		return nil, err
	}
	cc, err := addressList(d.CC, false)
	if err != nil {
		return nil, err
	}
	sender, err := addressList(from, true)
	if err != nil {
		return nil, fmt.Errorf("Your Gmail account address is unavailable")
	}
	if strings.ContainsAny(d.Subject+d.ReplyMessageID+d.References+d.DeliveryID, "\r\n") {
		return nil, fmt.Errorf("Message headers contain an invalid line break")
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: %s\r\n", sender, to, mime.QEncoding.Encode("UTF-8", d.Subject), time.Now().Format(time.RFC1123Z), d.DeliveryID)
	if cc != "" {
		fmt.Fprintf(&b, "Cc: %s\r\n", cc)
	}
	if d.ReplyMessageID != "" {
		fmt.Fprintf(&b, "In-Reply-To: %s\r\nReferences: %s\r\n", d.ReplyMessageID, strings.TrimSpace(d.References+" "+d.ReplyMessageID))
	}
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	w := quotedprintable.NewWriter(&b)
	if _, err = w.Write([]byte(d.Body)); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (c *Client) Send(ctx context.Context, d Draft, from string) (string, bool, error) {
	raw, err := BuildMIME(d, from)
	if err != nil {
		return "", false, err
	}
	payload := map[string]string{"raw": base64.RawURLEncoding.EncodeToString(raw)}
	if d.ThreadID != "" {
		payload["threadId"] = d.ThreadID
	}
	var out struct {
		ID string `json:"id"`
	}
	uncertain, err := c.api(ctx, "POST", "/messages/send", payload, &out)
	if err == nil && out.ID == "" {
		return "", true, errors.New("Gmail did not confirm delivery")
	}
	return out.ID, uncertain, err
}
