package mailapp

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"
)

func tlsListener(t *testing.T) (net.Listener, *x509.CertPool) {
	t.Helper()
	seed := httptest.NewTLSServer(nil)
	config := seed.TLS.Clone()
	roots := x509.NewCertPool()
	roots.AddCert(seed.Certificate())
	seed.Close()
	l, err := tls.Listen("tcp", "127.0.0.1:0", config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l, roots
}

func TestIMAPMailboxRoundTrip(t *testing.T) {
	l, roots := tlsListener(t)
	s := server.New(memory.New())
	go s.Serve(l)
	defer s.Close()
	a := IMAPAccount{Username: "username", Password: "password", Incoming: l.Addr().String(), Archive: "Archive", roots: roots}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	list, err := a.List(ctx, "inbox", "", "")
	if err != nil || len(list.Threads) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
	id := list.Threads[0].ID
	conv, err := a.Thread(ctx, id)
	if err != nil || len(conv.Messages) != 1 || !strings.Contains(conv.Messages[0].Body, "Hi there") {
		t.Fatalf("read: %v %v", conv, err)
	}
	if err := a.Modify(ctx, id, "star"); err != nil {
		t.Fatal(err)
	}
	starred, err := a.List(ctx, "starred", "", "")
	if err != nil || len(starred.Threads) != 1 || starred.Threads[0].ID != id {
		t.Fatal("flag change was not reflected", err)
	}
	if err := a.Modify(ctx, strings.Replace(id, "-1-", "-2-", 1), "unread"); err == nil {
		t.Fatal("accepted stale UIDVALIDITY")
	}
	if err := a.Modify(ctx, id, "archive"); err == nil {
		t.Fatal("archive should fail safely on server without MOVE")
	}
}

func TestSMTPDeliveryAcknowledgement(t *testing.T) {
	for _, ack := range []bool{true, false} {
		t.Run(fmt.Sprint(ack), func(t *testing.T) {
			l, roots := tlsListener(t)
			received := make(chan string, 1)
			go func() {
				conn, e := l.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				r := bufio.NewReader(conn)
				fmt.Fprint(conn, "220 test ESMTP\r\n")
				for {
					line, e := r.ReadString('\n')
					if e != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO "):
						fmt.Fprint(conn, "250-test\r\n250 AUTH PLAIN\r\n")
					case strings.HasPrefix(line, "AUTH PLAIN "):
						fmt.Fprint(conn, "235 accepted\r\n")
					case strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
						fmt.Fprint(conn, "250 ok\r\n")
					case line == "DATA\r\n":
						fmt.Fprint(conn, "354 go\r\n")
						var body strings.Builder
						for {
							part, e := r.ReadString('\n')
							if e != nil {
								return
							}
							if part == ".\r\n" {
								break
							}
							body.WriteString(part)
						}
						received <- body.String()
						if ack {
							fmt.Fprint(conn, "250 accepted\r\n")
						}
						return
					default:
						return
					}
				}
			}()
			a := IMAPAccount{Outgoing: l.Addr().String(), SMTPMode: "tls", Username: "user", Password: "fixture", roots: roots}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, uncertain, err := a.Send(ctx, Draft{To: "reader@example.org", Body: "hello", Subject: "fixture", DeliveryID: "<abc@example.org>"}, "writer@example.org")
			if (err == nil) != ack || uncertain == ack {
				t.Fatalf("ack=%v uncertain=%v err=%v", ack, uncertain, err)
			}
			if !strings.Contains(<-received, "Subject: fixture") {
				t.Fatal("missing message")
			}
		})
	}
}
