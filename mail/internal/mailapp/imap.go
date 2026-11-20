package mailapp

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/mail"
	"net/smtp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	_ "github.com/emersion/go-message/charset"
	msgmail "github.com/emersion/go-message/mail"
)

type mailBackend interface {
	List(context.Context, string, string, string) (Listing, error)
	Thread(context.Context, string) (Conversation, error)
	Modify(context.Context, string, string) error
	Send(context.Context, Draft, string) (string, bool, error)
}

type IMAPAccount struct {
	roots    *x509.CertPool
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
	Incoming string `json:"incoming"`
	Outgoing string `json:"outgoing"`
	SMTPMode string `json:"smtp_mode"`
	Sent     string `json:"sent_folder"`
	Archive  string `json:"archive_folder"`
}

func (a IMAPAccount) dialMail(ctx context.Context, addr string, implicit bool) (net.Conn, string, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return nil, "", errors.New("Invalid mail server address")
	}
	var conn net.Conn
	if implicit {
		d := tls.Dialer{Config: &tls.Config{ServerName: host, RootCAs: a.roots, MinVersion: tls.VersionTLS12}}
		conn, err = d.DialContext(ctx, "tcp", addr)
	} else {
		var d net.Dialer
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, "", errors.New("Could not connect securely to the mail server")
	}
	if end, ok := ctx.Deadline(); ok {
		conn.SetDeadline(end)
	}
	return conn, host, nil
}
func (a IMAPAccount) open(ctx context.Context, folder string, readonly bool) (*client.Client, *imap.MailboxStatus, func(), error) {
	box, err := a.folder(folder)
	if err != nil {
		return nil, nil, nil, err
	}
	conn, _, err := a.dialMail(ctx, a.Incoming, true)
	if err != nil {
		return nil, nil, nil, err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	closeConn := func() { stop(); conn.Close() }
	c, err := client.New(conn)
	if err == nil {
		c.ErrorLog = log.New(io.Discard, "", 0)
		err = c.Login(a.Username, a.Password)
	}
	if err != nil {
		closeConn()
		return nil, nil, nil, errors.New("IMAP sign-in failed. Check your account settings")
	}
	status, err := c.Select(box, readonly)
	if err != nil {
		closeConn()
		return nil, nil, nil, errors.New("Could not open that folder. Check its name in account setup")
	}
	return c, status, closeConn, nil
}
func (a IMAPAccount) folder(key string) (string, error) {
	switch key {
	case "inbox", "starred":
		return "INBOX", nil
	case "sent":
		return a.Sent, nil
	case "all":
		return a.Archive, nil
	}
	return "", errors.New("Unknown mail folder")
}
func (a IMAPAccount) id(folder string, valid, uid uint32) string {
	if folder == "starred" {
		folder = "inbox"
	}
	box, _ := a.folder(folder)
	h := sha256.Sum256([]byte(box))
	return fmt.Sprintf("%s-%s-%d-%d", folder, hex.EncodeToString(h[:8]), valid, uid)
}
func (a IMAPAccount) List(ctx context.Context, folder, query, page string) (Listing, error) {
	list := Listing{Threads: []Summary{}, Updated: time.Now().UTC().Format(time.RFC3339)}
	c, box, done, err := a.open(ctx, folder, true)
	if err != nil {
		return list, err
	}
	defer done()
	criteria := imap.NewSearchCriteria()
	if folder == "starred" {
		criteria.WithFlags = []string{imap.FlaggedFlag}
	}
	if query != "" {
		criteria.Text = []string{query}
	}
	if page != "" {
		last, err := strconv.ParseUint(page, 10, 32)
		if err != nil || last < 2 {
			return list, errors.New("Invalid page")
		}
		criteria.Uid = new(imap.SeqSet)
		criteria.Uid.AddRange(1, uint32(last-1))
	}
	ids, err := c.UidSearch(criteria)
	if err != nil {
		return list, errors.New("IMAP search failed")
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	if len(ids) > 20 {
		ids = ids[:20]
		list.Next = strconv.FormatUint(uint64(ids[19]), 10)
	}
	if len(ids) == 0 {
		return list, nil
	}
	set := new(imap.SeqSet)
	set.AddNum(ids...)
	ch := make(chan *imap.Message, 20)
	if err = c.UidFetch(set, []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchFlags}, ch); err != nil {
		return list, errors.New("Could not read message headers")
	}
	byUID := map[uint32]Summary{}
	for m := range ch {
		e := m.Envelope
		if e == nil {
			continue
		}
		from := ""
		if len(e.From) > 0 {
			from = (&mail.Address{Name: e.From[0].PersonalName, Address: e.From[0].Address()}).String()
		}
		byUID[m.Uid] = Summary{ID: a.id(folder, box.UidValidity, m.Uid), Subject: shortText(e.Subject, 1024), From: shortText(from, 512), Date: e.Date.Format(time.RFC1123Z), Unread: !contains(m.Flags, imap.SeenFlag), Starred: contains(m.Flags, imap.FlaggedFlag), Count: 1}
	}
	for _, uid := range ids {
		if s, ok := byUID[uid]; ok {
			list.Threads = append(list.Threads, s)
		}
	}
	return list, nil
}
func (a IMAPAccount) selectMessage(ctx context.Context, id string, readonly bool) (*client.Client, *imap.SeqSet, func(), error) {
	parts := strings.Split(id, "-")
	if len(parts) != 4 {
		return nil, nil, nil, errors.New("Invalid message")
	}
	uid, err := strconv.ParseUint(parts[3], 10, 32)
	if err != nil || uid == 0 {
		return nil, nil, nil, errors.New("Invalid message UID")
	}
	c, box, done, err := a.open(ctx, parts[0], readonly)
	if err != nil {
		return nil, nil, nil, err
	}
	if a.id(parts[0], box.UidValidity, uint32(uid)) != id {
		done()
		return nil, nil, nil, errors.New("Folder changed. Refresh before opening or updating this message")
	}
	set := new(imap.SeqSet)
	set.AddNum(uint32(uid))
	return c, set, done, nil
}
func (a IMAPAccount) Thread(ctx context.Context, id string) (Conversation, error) {
	c, set, done, err := a.selectMessage(ctx, id, true)
	if err != nil {
		return Conversation{}, err
	}
	defer done()
	const limit = 512 * 1024
	section := &imap.BodySectionName{Peek: true, Partial: []int{0, limit}}
	ch := make(chan *imap.Message, 1)
	if err = c.UidFetch(set, []imap.FetchItem{imap.FetchFlags, imap.FetchRFC822Size, section.FetchItem()}, ch); err != nil {
		return Conversation{}, errors.New("Could not retrieve message")
	}
	m := <-ch
	if m == nil || m.GetBody(section) == nil {
		return Conversation{}, errors.New("Message is no longer available")
	}
	r, err := msgmail.CreateReader(m.GetBody(section))
	if r == nil {
		return Conversation{}, errors.New("Could not decode message")
	}
	h := r.Header
	subject, _ := h.Subject()
	item := Message{ID: id, Subject: shortText(subject, 2048), From: shortText(h.Get("From"), 2048), To: shortText(h.Get("To"), 2048), ReplyTo: shortText(h.Get("Reply-To"), 2048), Date: h.Get("Date"), MessageID: shortText(h.Get("Message-ID"), 2048), References: shortText(h.Get("References"), 4096), Attachments: []string{}}
	if item.ReplyTo == "" {
		item.ReplyTo = item.From
	}
	var plain, html string
	shortened := m.Size > limit
	for {
		part, e := r.NextPart()
		if e == io.EOF {
			break
		}
		if part == nil {
			shortened = true
			break
		}
		switch header := part.Header.(type) {
		case *msgmail.AttachmentHeader:
			name, _ := header.Filename()
			if len(item.Attachments) < 40 {
				item.Attachments = append(item.Attachments, shortText(name, 256))
			}
		case *msgmail.InlineHeader:
			typ, _, _ := header.ContentType()
			body, e := io.ReadAll(io.LimitReader(part.Body, 128*1024+1))
			if len(body) > 128*1024 || e != nil {
				shortened = true
			}
			switch typ {
			case "text/plain":
				if plain == "" {
					plain = shortText(string(body), 128*1024)
				}
			case "text/html":
				if html == "" {
					html = HTMLText(string(body))
				}
			}
		}
	}
	item.Body = plain
	if item.Body == "" {
		item.Body = html
	}
	return fitConversation(Conversation{ID: id, Subject: item.Subject, Messages: []Message{item}, Unread: !contains(m.Flags, imap.SeenFlag), Starred: contains(m.Flags, imap.FlaggedFlag), Updated: time.Now().UTC().Format(time.RFC3339), Shortened: shortened}), nil
}
func (a IMAPAccount) Modify(ctx context.Context, id, op string) error {
	c, set, done, err := a.selectMessage(ctx, id, false)
	if err != nil {
		return err
	}
	defer done()
	if op == "archive" {
		if strings.HasPrefix(id, "all-") {
			return errors.New("Already in Archive")
		}
		if ok, e := c.Support("MOVE"); e != nil || !ok {
			return errors.New("This server does not support safe archiving with IMAP MOVE")
		}
		err = c.UidMove(set, a.Archive)
	} else {
		var flag string
		var mode imap.FlagsOp = imap.AddFlags
		switch op {
		case "star":
			flag = imap.FlaggedFlag
		case "unstar":
			flag = imap.FlaggedFlag
			mode = imap.RemoveFlags
		case "read":
			flag = imap.SeenFlag
		case "unread":
			flag = imap.SeenFlag
			mode = imap.RemoveFlags
		default:
			return errors.New("Unknown message action")
		}
		err = c.UidStore(set, imap.FormatFlagsOp(mode, true), []interface{}{flag}, nil)
	}
	if err != nil {
		return errors.New("The server could not apply the change")
	}
	return nil
}
func (a IMAPAccount) Send(ctx context.Context, d Draft, from string) (string, bool, error) {
	raw, err := BuildMIME(d, from)
	if err != nil {
		return "", false, err
	}
	if a.SMTPMode != "tls" && a.SMTPMode != "starttls" {
		return "", false, errors.New("Choose a secure SMTP mode")
	}
	conn, host, err := a.dialMail(ctx, a.Outgoing, a.SMTPMode == "tls")
	if err != nil {
		return "", false, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	c, err := smtp.NewClient(conn, host)
	if err == nil && a.SMTPMode == "starttls" {
		err = c.StartTLS(&tls.Config{ServerName: host, RootCAs: a.roots, MinVersion: tls.VersionTLS12})
	}
	if err != nil {
		return "", false, errors.New("Could not establish secure SMTP connection")
	}
	if err = c.Auth(smtp.PlainAuth("", a.Username, a.Password, host)); err != nil {
		return "", false, errors.New("SMTP sign-in failed")
	}
	if err = c.Mail(from); err != nil {
		return "", false, errors.New("SMTP rejected the sender")
	}
	for _, field := range []string{d.To, d.CC} {
		if strings.TrimSpace(field) == "" {
			continue
		}
		addresses, err := mail.ParseAddressList(field)
		if err != nil {
			return "", false, errors.New("Invalid recipient")
		}
		for _, addr := range addresses {
			if err = c.Rcpt(addr.Address); err != nil {
				return "", false, errors.New("SMTP rejected a recipient")
			}
		}
	}
	writer, err := c.Data()
	if err != nil {
		return "", false, errors.New("SMTP refused the message")
	}
	if _, err = writer.Write(raw); err != nil {
		return "", false, errors.New("Could not upload the message")
	}
	if err = writer.Close(); err != nil {
		return "", true, errors.New("SMTP delivery could not be confirmed")
	}
	return d.DeliveryID, false, nil
}
