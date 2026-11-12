package mailapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

type Credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	AccessToken  string `json:"access_token,omitempty"`
	Expiry       int64  `json:"expiry,omitempty"`
	Email        string `json:"email,omitempty"`
}

type Summary struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	From    string `json:"from"`
	Snippet string `json:"snippet"`
	Date    string `json:"date"`
	Unread  bool   `json:"unread"`
	Starred bool   `json:"starred"`
	Count   int    `json:"count"`
}

type Message struct {
	ID          string   `json:"id"`
	From        string   `json:"from"`
	To          string   `json:"to"`
	ReplyTo     string   `json:"replyTo"`
	Subject     string   `json:"subject"`
	Date        string   `json:"date"`
	Body        string   `json:"body"`
	MessageID   string   `json:"messageId"`
	References  string   `json:"references"`
	Attachments []string `json:"attachments"`
}

type Conversation struct {
	ID        string    `json:"id"`
	Subject   string    `json:"subject"`
	Messages  []Message `json:"messages"`
	Unread    bool      `json:"unread"`
	Starred   bool      `json:"starred"`
	Cached    bool      `json:"cached"`
	Updated   string    `json:"updated"`
	Shortened bool      `json:"shortened"`
}

type Listing struct {
	Threads []Summary `json:"threads"`
	Next    string    `json:"next"`
	Cached  bool      `json:"cached"`
	Updated string    `json:"updated"`
	Warning string    `json:"warning,omitempty"`
}

type Draft struct {
	ID             string `json:"id"`
	To             string `json:"to"`
	CC             string `json:"cc"`
	Subject        string `json:"subject"`
	Body           string `json:"body"`
	ThreadID       string `json:"threadId"`
	ReplyMessageID string `json:"replyMessageId"`
	References     string `json:"references"`
	Revision       int64  `json:"revision"`
	Updated        string `json:"updated"`
	State          string `json:"state"`
	DeliveryID     string `json:"deliveryId,omitempty"`
	RemoteID       string `json:"remoteId,omitempty"`
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,160}$`)

type Store struct {
	Dir string
	mu  sync.Mutex
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) Read(name string, dest any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(s.Dir, name))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dest)
}

func (s *Store) Write(name string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(name, value)
}

func (s *Store) write(name string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, ".pending-*")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(path, filepath.Join(s.Dir, name))
	}
	if err == nil {
		d, e := os.Open(s.Dir)
		if e != nil {
			return e
		}
		defer d.Close()
		err = d.Sync()
	}
	return err
}

func (s *Store) Draft(id string) (Draft, error) {
	var d Draft
	if !safeID.MatchString(id) {
		return d, fmt.Errorf("Invalid draft")
	}
	err := s.Read("draft-"+id+".json", &d)
	// No automatic replay after an interrupted send: the delivery may have succeeded.
	if d.State == "sending" {
		d.State = "uncertain"
	}
	return d, err
}

func (s *Store) SaveDraft(d Draft) (Draft, error) {
	if !safeID.MatchString(d.ID) {
		return d, fmt.Errorf("Invalid draft")
	}
	if len(d.Body) > 256*1024 || len(d.To)+len(d.CC) > 8192 || len(d.Subject) > 4096 {
		return d, fmt.Errorf("Draft is too large")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var old Draft
	b, err := os.ReadFile(filepath.Join(s.Dir, "draft-"+d.ID+".json"))
	if err == nil {
		if json.Unmarshal(b, &old) != nil {
			return d, fmt.Errorf("Could not read the saved draft; it has been preserved")
		}
		if old.State != "draft" {
			return old, fmt.Errorf("This draft has already been submitted. Check Sent in Gmail")
		}
		if d.Revision <= old.Revision {
			return old, nil
		}
	} else if !os.IsNotExist(err) {
		return d, err
	}
	d.State = "draft"
	d.DeliveryID = ""
	d.RemoteID = ""
	d.Updated = time.Now().UTC().Format(time.RFC3339)
	return d, s.write("draft-"+d.ID+".json", d)
}

func (s *Store) Drafts() ([]Draft, error) {
	paths, err := filepath.Glob(filepath.Join(s.Dir, "draft-*.json"))
	if err != nil {
		return nil, err
	}
	result := []Draft{}
	for _, path := range paths {
		var d Draft
		if err := s.Read(filepath.Base(path), &d); err != nil {
			return nil, fmt.Errorf("A saved draft could not be read. Its file has been preserved")
		}
		if d.State == "sending" {
			d.State = "uncertain"
		}
		if d.State != "sent" {
			result = append(result, d)
		}
	}
	return result, nil
}
