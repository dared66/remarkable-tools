package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"remarkable-mail/internal/mailapp"
)

func main() {
	dir := flag.String("data-dir", filepath.Join(os.Getenv("HOME"), ".remarkmail"), "Private local mail storage")
	demo := flag.Bool("demo", false, "Use a synthetic mailbox; never contact Gmail")
	stdio := flag.Bool("stdio", false, "JSON line transport for desktop preview")
	flag.Parse()
	app, err := mailapp.NewApp(*dir, *demo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not open private mail storage")
		os.Exit(1)
	}
	if *stdio {
		err = runStdio(app)
	} else if flag.NArg() == 1 {
		err = runAppLoad(app, flag.Arg(0))
	} else {
		err = fmt.Errorf("AppLoad socket or --stdio is required")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Mail backend stopped:", err)
		os.Exit(1)
	}
}

func respond(app *mailapp.App, b []byte) []byte {
	var req mailapp.Request
	if json.Unmarshal(b, &req) != nil {
		out, _ := json.Marshal(mailapp.Response{Error: "Invalid mail request"})
		return out
	}
	res := app.Handle(req)
	out, err := json.Marshal(res)
	if err != nil {
		return []byte(`{"id":0,"ok":false,"error":"Could not encode mail response"}`)
	}
	if len(out) > 180*1024 {
		out, _ = json.Marshal(mailapp.Response{ID: req.ID, Error: "This item is too large for the tablet. Open the full item in Gmail on your phone or computer"})
	}
	return out
}

func runStdio(app *mailapp.App) error {
	s := bufio.NewScanner(os.Stdin)
	s.Buffer(make([]byte, 4096), 1024*1024)
	w := bufio.NewWriter(os.Stdout)
	for s.Scan() {
		if _, err := w.Write(append(respond(app, s.Bytes()), '\n')); err != nil {
			return err
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
	if err := s.Err(); err != nil && err != io.EOF {
		return err
	}
	return nil
}
