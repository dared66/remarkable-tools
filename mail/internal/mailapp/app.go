package mailapp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Request struct {
	ID     int64           `json:"id"`
	Action string          `json:"action"`
	Params json.RawMessage `json:"params"`
}
type Response struct {
	ID     int64  `json:"id"`
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}
type Params struct {
	Folder    string `json:"folder"`
	Query     string `json:"query"`
	Page      string `json:"page"`
	ID        string `json:"id"`
	Operation string `json:"operation"`
	Draft     Draft  `json:"draft"`
	Confirm   bool   `json:"confirm"`
	Revision  int64  `json:"revision"`
}

type App struct {
	Root   *Store
	Store  *Store
	Client *Client
	Demo   bool
	Email  string
}

func NewApp(dir string, demo bool) (*App, error) {
	s, err := NewStore(dir)
	if err != nil {
		return nil, err
	}
	a := &App{Root: s, Client: NewClient(s), Demo: demo}
	if err = a.account(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *App) account() error {
	var cr Credentials
	_ = a.Root.Read("credentials.json", &cr)
	email := cr.Email
	if a.Demo {
		email = "preview@example.com"
	}
	if a.Store != nil && a.Email == email {
		return nil
	}
	a.Email = email
	a.Client.ExpectedEmail = email
	key := "unconnected"
	if email != "" {
		h := sha256.Sum256([]byte(strings.ToLower(email)))
		key = hex.EncodeToString(h[:16])
	}
	s, err := NewStore(filepath.Join(a.Root.Dir, "accounts", key))
	if err == nil {
		a.Store = s
	}
	return err
}

func (a *App) configured() bool {
	var cr Credentials
	return a.Demo || (a.Root.Read("credentials.json", &cr) == nil && cr.RefreshToken != "" && cr.ClientID != "" && cr.Email != "")
}

func (a *App) Handle(req Request) Response {
	res := Response{ID: req.ID}
	if err := a.account(); err != nil {
		res.Error = "Could not open local mail storage"
		return res
	}
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	var p Params
	if len(req.Params) > 0 && json.Unmarshal(req.Params, &p) != nil {
		res.Error = "Invalid mail request"
		return res
	}
	value, err := a.perform(ctx, req.Action, p)
	if err != nil {
		res.Error = err.Error()
	} else {
		res.OK = true
		res.Result = value
	}
	return res
}

type cachedListing struct {
	Listing
	Folder string `json:"folder"`
}

func cacheKey(p Params) string {
	h := sha256.Sum256([]byte(p.Folder + "\x00" + p.Query + "\x00" + p.Page))
	return "list-" + hex.EncodeToString(h[:16]) + ".json"
}
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("Secure random source unavailable")
	}
	return hex.EncodeToString(b[:])
}

func (a *App) perform(ctx context.Context, action string, p Params) (any, error) {
	if action == "status" {
		return map[string]any{"configured": a.configured(), "email": a.Email, "demo": a.Demo}, nil
	}
	if !a.configured() {
		return nil, fmt.Errorf("Connect Gmail using the setup tool on your Mac")
	}
	switch action {
	case "list":
		if a.Demo {
			return demoListing(p), nil
		}
		var old cachedListing
		cached := a.Store.Read(cacheKey(p), &old) == nil
		list, err := a.Client.List(ctx, p.Folder, p.Query, p.Page)
		if err != nil && cached {
			old.Cached = true
			old.Warning = err.Error()
			return old.Listing, nil
		}
		if err != nil {
			return nil, err
		}
		if err = a.Store.Write(cacheKey(p), cachedListing{Listing: list, Folder: p.Folder}); err != nil {
			return nil, fmt.Errorf("Could not save the mailbox for offline use")
		}
		return list, nil
	case "thread":
		if !safeID.MatchString(p.ID) {
			return nil, fmt.Errorf("Invalid conversation")
		}
		if a.Demo {
			return demoThread(p.ID), nil
		}
		thread, err := a.Client.Thread(ctx, p.ID)
		if err != nil {
			if a.Store.Read("thread-"+p.ID+".json", &thread) == nil {
				thread.Cached = true
				return thread, nil
			}
			return nil, err
		}
		if err = a.Store.Write("thread-"+p.ID+".json", thread); err != nil {
			return nil, fmt.Errorf("Could not save this conversation for offline use")
		}
		return thread, nil
	case "modify":
		if a.Demo {
			return map[string]bool{"updated": true}, nil
		}
		if err := a.Client.Modify(ctx, p.ID, p.Operation); err != nil {
			return nil, err
		}
		a.updateCaches(p.ID, p.Operation)
		return map[string]bool{"updated": true}, nil
	case "draft-new":
		d := p.Draft
		d.ID = newID()
		d.Revision = 1
		return a.Store.SaveDraft(d)
	case "draft-save":
		return a.Store.SaveDraft(p.Draft)
	case "draft-list":
		ds, err := a.Store.Drafts()
		sort.Slice(ds, func(i, j int) bool { return ds[i].Updated > ds[j].Updated })
		return ds, err
	case "draft-load":
		return a.Store.Draft(p.ID)
	case "draft-delete":
		d, err := a.Store.Draft(p.ID)
		if err != nil {
			return nil, fmt.Errorf("Could not open draft")
		}
		if d.State != "draft" {
			return nil, fmt.Errorf("Keep this delivery record until you have checked Sent in Gmail")
		}
		if err = os.Remove(filepath.Join(a.Store.Dir, "draft-"+p.ID+".json")); err != nil {
			return nil, fmt.Errorf("Could not delete draft")
		}
		return map[string]bool{"deleted": true}, nil
	case "send":
		return a.send(ctx, p)
	default:
		return nil, fmt.Errorf("Unknown mail request")
	}
}

func (a *App) send(ctx context.Context, p Params) (any, error) {
	if !p.Confirm {
		return nil, fmt.Errorf("Confirm sending this email first")
	}
	d, err := a.Store.Draft(p.ID)
	if err != nil {
		return nil, fmt.Errorf("Could not open the saved draft")
	}
	if d.State != "draft" {
		return nil, fmt.Errorf("This message may already have been sent. Check Sent in Gmail")
	}
	if d.Revision != p.Revision {
		return nil, fmt.Errorf("The draft changed. Save and review it before sending")
	}
	d.DeliveryID = "<" + newID() + "@remarkmail.local>"
	if _, err = BuildMIME(d, a.Email); err != nil {
		return nil, err
	}
	if strings.TrimSpace(d.Body) == "" {
		return nil, fmt.Errorf("Write a message before sending")
	}
	d.State = "sending"
	if a.Store.Write("draft-"+d.ID+".json", d) != nil {
		return nil, fmt.Errorf("Could not save the delivery record. Nothing was sent")
	}
	remote, uncertain, err := "preview-delivery", false, error(nil)
	if !a.Demo {
		remote, uncertain, err = a.Client.Send(ctx, d, a.Email)
	}
	if err != nil {
		if uncertain {
			d.State = "uncertain"
		} else {
			d.State = "draft"
		}
		if a.Store.Write("draft-"+d.ID+".json", d) != nil {
			return nil, fmt.Errorf("Delivery status could not be saved. Check Sent in Gmail before trying again")
		}
		if uncertain {
			return nil, fmt.Errorf("Delivery is uncertain. Check Sent in Gmail before sending another copy")
		}
		return nil, err
	}
	d.State = "sent"
	d.RemoteID = remote
	if a.Store.Write("draft-"+d.ID+".json", d) != nil {
		return nil, fmt.Errorf("Gmail accepted this email, but its receipt could not be saved. Check Sent; do not send again")
	}
	return map[string]any{"sent": true, "demo": a.Demo}, nil
}

func (a *App) updateCaches(id, operation string) {
	paths, _ := filepath.Glob(filepath.Join(a.Store.Dir, "list-*.json"))
	for _, path := range paths {
		var l cachedListing
		if a.Store.Read(filepath.Base(path), &l) != nil {
			continue
		}
		result := []Summary{}
		for _, s := range l.Threads {
			if s.ID == id {
				if operation == "archive" && l.Folder == "inbox" || operation == "unstar" && l.Folder == "starred" {
					continue
				}
				if operation == "read" {
					s.Unread = false
				}
				if operation == "unread" {
					s.Unread = true
				}
				if operation == "star" {
					s.Starred = true
				}
				if operation == "unstar" {
					s.Starred = false
				}
			}
			result = append(result, s)
		}
		l.Threads = result
		_ = a.Store.Write(filepath.Base(path), l)
	}
	var t Conversation
	name := "thread-" + id + ".json"
	if a.Store.Read(name, &t) == nil {
		if operation == "read" {
			t.Unread = false
		}
		if operation == "unread" {
			t.Unread = true
		}
		if operation == "star" {
			t.Starred = true
		}
		if operation == "unstar" {
			t.Starred = false
		}
		_ = a.Store.Write(name, t)
	}
}

func demoThread(id string) Conversation {
	return Conversation{ID: id, Subject: "A plan for Saturday", Updated: time.Now().UTC().Format(time.RFC3339), Unread: true, Messages: []Message{
		{ID: "sample-1", From: "Alex Morgan <alex@example.com>", To: "preview@example.com", ReplyTo: "Alex Morgan <alex@example.com>", Subject: "A plan for Saturday", Date: "Sat, 7 Nov 2026 09:15:00 -0800", Body: "Hi,\n\nWould you like to meet at the park on Saturday morning? We could bring coffee and something from the bakery.\n\nThere is no rush — around ten would be lovely.\n\nAlex", MessageID: "<sample-1@example.com>", Attachments: []string{}},
		{ID: "sample-2", From: "Alex Morgan <alex@example.com>", To: "preview@example.com", ReplyTo: "Alex Morgan <alex@example.com>", Subject: "Re: A plan for Saturday", Date: "Wed, 11 Nov 2026 16:42:00 -0800", Body: "One more thought: if it rains, we can meet at the café instead.\n\nLet me know what works for you.\n\nAlex", MessageID: "<sample-2@example.com>", References: "<sample-1@example.com>", Attachments: []string{"park-map.pdf"}},
	}}
}

func demoListing(p Params) Listing {
	threads := []Summary{{ID: "sample-saturday", Subject: "A plan for Saturday", From: "Alex Morgan <alex@example.com>", Snippet: "If it rains, we can meet at the café instead. Let me know what works for you.", Date: "Wed, 11 Nov 2026 16:42:00 -0800", Unread: true, Count: 2}, {ID: "sample-book", Subject: "The book we talked about", From: "Sam <sam@example.com>", Snippet: "I finished it last night. The last chapter is the one I keep thinking about.", Date: "Wed, 11 Nov 2026 11:12:00 -0800", Starred: true, Count: 1}, {ID: "sample-market", Subject: "This week at the market", From: "Neighborhood Market <hello@example.com>", Snippet: "Apples, winter greens, and bread fresh from the oven. See you this weekend.", Date: "Tue, 10 Nov 2026 08:30:00 -0800", Count: 1}}
	result := []Summary{}
	for _, s := range threads {
		if p.Folder == "starred" && !s.Starred {
			continue
		}
		if p.Query != "" && !strings.Contains(strings.ToLower(s.Subject+" "+s.Snippet+" "+s.From), strings.ToLower(p.Query)) {
			continue
		}
		result = append(result, s)
	}
	return Listing{Threads: result, Updated: time.Now().UTC().Format(time.RFC3339)}
}
