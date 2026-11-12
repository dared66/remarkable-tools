package mailapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeApp(t *testing.T) *App {
	t.Helper()
	a, e := NewApp(t.TempDir(), false)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.Root.Write("credentials.json", Credentials{ClientID: "test-client", RefreshToken: "test-refresh", AccessToken: "test-access", Expiry: time.Now().Unix() + 3600, Email: "me@example.com"}); e != nil {
		t.Fatal(e)
	}
	if e = a.account(); e != nil {
		t.Fatal(e)
	}
	return a
}
func call(t *testing.T, a *App, action string, p Params) Response {
	t.Helper()
	b, _ := json.Marshal(p)
	return a.Handle(Request{ID: 42, Action: action, Params: b})
}
func saved(t *testing.T, a *App) Draft {
	t.Helper()
	d, e := a.Store.SaveDraft(Draft{ID: "test-draft", To: "Alex <alex@example.com>", Subject: "Café on Saturday", Body: "Hello — see you at ten.\nThanks!", Revision: 1})
	if e != nil {
		t.Fatal(e)
	}
	return d
}

func TestMIMEUnicodeThreadingAndHeaderInjection(t *testing.T) {
	d := Draft{To: "Alex <alex@example.com>", CC: "sam@example.com", Subject: "Café — Saturday", Body: "Hello ☕\nSee you bientôt.", DeliveryID: "<unique@remarkmail.local>", ReplyMessageID: "<original@example.com>", References: "<earlier@example.com>"}
	b, e := BuildMIME(d, "me@example.com")
	if e != nil {
		t.Fatal(e)
	}
	m, e := mail.ReadMessage(bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	if m.Header.Get("In-Reply-To") != d.ReplyMessageID || !strings.Contains(m.Header.Get("References"), d.References) {
		t.Fatal("Threading headers lost")
	}
	decoded, e := io.ReadAll(quotedprintable.NewReader(m.Body))
	if e != nil || !strings.Contains(string(decoded), "bientôt") {
		t.Fatal("Unicode body damaged")
	}
	for _, bad := range []Draft{{To: "a@example.com\r\nBcc: b@example.com"}, {To: "a@example.com", Subject: "hello\nBcc: b@example.com"}, {To: "a@example.com", ReplyMessageID: "<ok>\r\nInjected: yes"}, {To: "not-an-email"}} {
		if _, e := BuildMIME(bad, "me@example.com"); e == nil {
			t.Fatal("Invalid header accepted")
		}
	}
}

func TestMessageDecodingAndUntrustedHTML(t *testing.T) {
	html := `<head><style>secret-css</style></head><div>Hello &amp; goodbye<script>secret-script</script><img src="https://tracker.invalid/x"></div><p>Next line</p>`
	text := HTMLText(html)
	if text != "Hello & goodbye\n\nNext line" {
		t.Fatalf("Unexpected HTML text %q", text)
	}
	p := Part{MimeType: "multipart/alternative", Parts: []Part{{MimeType: "text/html"}, {MimeType: "text/plain"}}}
	p.Parts[0].Body.Data = base64.RawURLEncoding.EncodeToString([]byte("<p>HTML version</p>"))
	p.Parts[1].Body.Data = base64.RawURLEncoding.EncodeToString([]byte("Plain preferred"))
	body, _ := bodyText(p)
	if body != "Plain preferred" {
		t.Fatal("Plain alternative not preferred")
	}
	latin := Part{MimeType: "text/plain", Headers: []Header{{Name: "Content-Type", Value: "text/plain; charset=windows-1252"}}}
	latin.Body.Data = base64.RawURLEncoding.EncodeToString([]byte{'c', 'a', 'f', 0xe9})
	if decodePart(latin) != "café" {
		t.Fatal("Declared charset not decoded")
	}
	attachment := Part{MimeType: "text/plain", Filename: "note.txt"}
	attachment.Body.Data = base64.RawURLEncoding.EncodeToString([]byte("not inline"))
	body, files := bodyText(attachment)
	if body != "" || len(files) != 1 {
		t.Fatal("Attachment treated as inline text")
	}
}

func TestDraftPersistenceAndStaleWrite(t *testing.T) {
	a := makeApp(t)
	d := saved(t, a)
	d.Body = "newest"
	d.Revision = 3
	if _, e := a.Store.SaveDraft(d); e != nil {
		t.Fatal(e)
	}
	d.Body = "stale"
	d.Revision = 2
	got, e := a.Store.SaveDraft(d)
	if e != nil || got.Body != "newest" {
		t.Fatal("Stale update overwrote newer draft")
	}
	s, e := NewStore(a.Store.Dir)
	if e != nil {
		t.Fatal(e)
	}
	got, e = s.Draft(d.ID)
	if e != nil || got.Body != "newest" {
		t.Fatal("Draft not durable")
	}
	if info, _ := os.Stat(filepath.Join(s.Dir, "draft-"+d.ID+".json")); info.Mode().Perm() != 0600 {
		t.Fatal("Draft file permissions")
	}
	d.ID = "../escape"
	if _, e := s.SaveDraft(d); e == nil {
		t.Fatal("Path traversal accepted")
	}
}

func TestSendConfirmationAndDuplicatePrevention(t *testing.T) {
	a := makeApp(t)
	d := saved(t, a)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/messages/send" {
			t.Errorf("Unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-access" {
			t.Error("Missing authorization")
		}
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		raw, _ := base64.RawURLEncoding.DecodeString(payload["raw"])
		if !strings.Contains(string(raw), "Message-ID:") {
			t.Error("Missing delivery identifier")
		}
		io.WriteString(w, `{"id":"sent-123"}`)
	}))
	defer server.Close()
	a.Client.BaseURL = server.URL
	if res := call(t, a, "send", Params{ID: d.ID, Revision: 1}); res.OK {
		t.Fatal("Send without confirmation")
	}
	if res := call(t, a, "send", Params{ID: d.ID, Confirm: true, Revision: 9}); res.OK {
		t.Fatal("Send with stale revision")
	}
	if requests != 0 {
		t.Fatal("Unconfirmed send reached Gmail")
	}
	if res := call(t, a, "send", Params{ID: d.ID, Confirm: true, Revision: 1}); !res.OK {
		t.Fatal(res.Error)
	}
	if res := call(t, a, "send", Params{ID: d.ID, Confirm: true, Revision: 1}); res.OK {
		t.Fatal("Duplicate send accepted")
	}
	if requests != 1 {
		t.Fatalf("Expected one send, got %d", requests)
	}
	ds, e := a.Store.Drafts()
	if e != nil || len(ds) != 0 {
		t.Fatal("Sent message remains in drafts")
	}
}

func TestUncertainSendIsNeverReplayed(t *testing.T) {
	a := makeApp(t)
	d := saved(t, a)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(503)
		io.WriteString(w, "backend unavailable")
	}))
	defer server.Close()
	a.Client.BaseURL = server.URL
	res := call(t, a, "send", Params{ID: d.ID, Revision: 1, Confirm: true})
	if res.OK || !strings.Contains(res.Error, "uncertain") {
		t.Fatal(res)
	}
	if res = call(t, a, "send", Params{ID: d.ID, Revision: 1, Confirm: true}); res.OK || requests != 1 {
		t.Fatal("Uncertain delivery replayed")
	}
	d, _ = a.Store.Draft(d.ID)
	if d.State != "uncertain" {
		t.Fatal(d.State)
	}
	d.State = "sending"
	_ = a.Store.Write("draft-"+d.ID+".json", d)
	recovered, e := a.Store.Draft(d.ID)
	if e != nil || recovered.State != "uncertain" {
		t.Fatal("Interrupted send not protected")
	}
}

func TestRejectedSendRemainsEditable(t *testing.T) {
	a := makeApp(t)
	d := saved(t, a)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) }))
	defer server.Close()
	a.Client.BaseURL = server.URL
	res := call(t, a, "send", Params{ID: d.ID, Revision: 1, Confirm: true})
	if res.OK {
		t.Fatal("Rejected send reported success")
	}
	d, _ = a.Store.Draft(d.ID)
	if d.State != "draft" {
		t.Fatal("Definitively rejected send should remain editable")
	}
}

func TestTokenRefreshAndCredentialRedaction(t *testing.T) {
	a := makeApp(t)
	_ = a.Root.Write("credentials.json", Credentials{ClientID: "test-client", ClientSecret: "private-client-secret", RefreshToken: "private-refresh-token", Email: "me@example.com"})
	tokens := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokens++
			_ = r.ParseForm()
			if r.Form.Get("refresh_token") != "private-refresh-token" {
				t.Error("Missing refresh token")
			}
			io.WriteString(w, `{"access_token":"new-private-token","expires_in":3600}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer new-private-token" {
			t.Error("Refreshed token not used")
		}
		io.WriteString(w, `{"emailAddress":"me@example.com"}`)
	}))
	defer server.Close()
	a.Client.BaseURL = server.URL
	a.Client.TokenURL = server.URL + "/token"
	for i := 0; i < 2; i++ {
		if _, e := a.Client.Profile(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	if tokens != 1 {
		t.Fatal("Token refreshed redundantly")
	}
	status := call(t, a, "status", Params{})
	b, _ := json.Marshal(status)
	if strings.Contains(string(b), "private") {
		t.Fatal("Token leaked in status")
	}
}

func TestOfflineCacheAndAccountIsolation(t *testing.T) {
	a := makeApp(t)
	p := Params{Folder: "inbox"}
	l := Listing{Threads: []Summary{{ID: "t1", Subject: "Saved mail"}}, Updated: "2026-11-11T12:00:00Z"}
	_ = a.Store.Write(cacheKey(p), cachedListing{Listing: l, Folder: "inbox"})
	_ = a.Store.Write("thread-t1.json", Conversation{ID: "t1", Messages: []Message{{Body: "Saved content"}}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	a.Client.BaseURL = server.URL
	res := call(t, a, "list", p)
	if !res.OK || !res.Result.(Listing).Cached {
		t.Fatal("Offline mailbox unavailable", res)
	}
	res = call(t, a, "thread", Params{ID: "t1"})
	if !res.OK || !res.Result.(Conversation).Cached {
		t.Fatal("Offline thread unavailable")
	}
	a.updateCaches("t1", "archive")
	res = call(t, a, "list", p)
	if len(res.Result.(Listing).Threads) != 0 {
		t.Fatal("Archived thread remains in offline inbox")
	}
	oldDir := a.Store.Dir
	_ = a.Root.Write("credentials.json", Credentials{ClientID: "test", RefreshToken: "test", Email: "other@example.com"})
	_ = a.account()
	if oldDir == a.Store.Dir {
		t.Fatal("Accounts share cache")
	}
	res = call(t, a, "thread", Params{ID: "t1"})
	if res.OK {
		t.Fatal("Other account can see cached conversation")
	}
}

func TestListPaginationLabelsAndMetadata(t *testing.T) {
	a := makeApp(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/threads":
			if r.URL.Query().Get("pageToken") != "page-two" || r.URL.Query().Get("q") != "from:alex" || r.URL.Query().Get("labelIds") != "STARRED" {
				t.Error("Search, folder, or page lost")
			}
			io.WriteString(w, `{"threads":[{"id":"t1"}],"nextPageToken":"page-three"}`)
		case "/threads/t1":
			if r.URL.Query().Get("format") != "metadata" {
				t.Error("List should fetch metadata only")
			}
			io.WriteString(w, `{"id":"t1","messages":[{"id":"m1","labelIds":["UNREAD","STARRED"],"snippet":"Hello &amp; welcome","payload":{"headers":[{"name":"Subject","value":"A letter"},{"name":"From","value":"Alex <alex@example.com>"}]}}]}`)
		default:
			t.Error("Unexpected API endpoint")
		}
	}))
	defer server.Close()
	a.Client.BaseURL = server.URL
	l, e := a.Client.List(context.Background(), "starred", "from:alex", "page-two")
	if e != nil {
		t.Fatal(e)
	}
	if l.Next != "page-three" || len(l.Threads) != 1 || !l.Threads[0].Unread || !l.Threads[0].Starred || l.Threads[0].Snippet != "Hello & welcome" {
		t.Fatal(l)
	}
}

func TestUnconfiguredAndDemoNeverContactGmail(t *testing.T) {
	a, e := NewApp(t.TempDir(), false)
	if e != nil {
		t.Fatal(e)
	}
	if res := call(t, a, "list", Params{Folder: "inbox"}); res.OK {
		t.Fatal("Unconfigured mailbox accepted")
	}
	demo, e := NewApp(t.TempDir(), true)
	if e != nil {
		t.Fatal(e)
	}
	demo.Client.BaseURL = "http://127.0.0.1:1"
	demo.Client.TokenURL = "http://127.0.0.1:1"
	if res := call(t, demo, "list", Params{Folder: "inbox"}); !res.OK {
		t.Fatal(res)
	}
	d := saved(t, demo)
	if res := call(t, demo, "send", Params{ID: d.ID, Revision: 1, Confirm: true}); !res.OK {
		t.Fatal(res)
	}
}

func TestAccountSwitchCannotUseAnotherAccountsToken(t *testing.T) {
	a := makeApp(t)
	var cr Credentials
	_ = a.Root.Read("credentials.json", &cr)
	cr.Email = "other@example.com"
	_ = a.Root.Write("credentials.json", cr)
	_, err := a.Client.token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "account changed") {
		t.Fatal("A different account's token was accepted")
	}
}

func TestLargeConversationFitsAppLoadPackets(t *testing.T) {
	remote := GmailThread{ID: "large-thread"}
	for i := 0; i < 50; i++ {
		m := GmailMessage{ID: "message", Payload: Part{MimeType: "text/plain"}}
		m.Payload.Body.Data = base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("Long content ☕\n", 10000)))
		remote.Messages = append(remote.Messages, m)
	}
	c := convertThread(remote)
	b, _ := json.Marshal(c)
	if len(b) > 160*1024 || !c.Shortened || len(c.Messages) == 0 {
		t.Fatal("Conversation exceeds a safe Unix packet size")
	}
}
