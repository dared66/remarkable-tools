package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	groupID = "00000000-0000-0000-0000-000000000001"
	docID   = "00000000-0000-0000-0000-000000000002"
	trashID = "00000000-0000-0000-0000-000000000003"
	lostID  = "00000000-0000-0000-0000-000000000004"
)

func fixture(t *testing.T) (*finder, func(string, string)) {
	t.Helper()
	base := t.TempDir()
	put := func(p, s string) {
		t.Helper()
		p = filepath.Join(base, p)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}
	meta := func(id, name, parent, kind string) {
		raw, _ := json.Marshal(map[string]any{"visibleName": name, "parent": parent, "type": kind, "lastModified": "1750000000000"})
		put(docsDir+"/"+id+".metadata", string(raw))
	}
	meta(groupID, "Recipes", "", "CollectionType")
	meta(docID, "Pastry / Notes", groupID, "DocumentType")
	meta(trashID, "Deleted folder", "trash", "CollectionType")
	meta(lostID, "Lost recipe", trashID, "DocumentType")
	put(docsDir+"/"+docID+".pdf", "%PDF-1.7")
	put(docsDir+"/"+docID+".content", `{"fileType":"pdf","pageCount":3,"cPages":{"pages":[{"id":"b","idx":{"value":"bb"}},{"id":"a","idx":{"value":"ba"}},{"id":"c","idx":{"value":"bc"},"deleted":{"value":true}}]}}`)
	for _, p := range []string{"a", "b", "c"} {
		put(docsDir+"/"+docID+".thumbnails/"+p+".png", "fixture")
	}
	put(vaultDir+"/Culinary/Notes/Café.md", "☕\n")
	put(vaultDir+"/Culinary/.private/Café.md", "hidden")
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return &finder{fs: root}, put
}

func ask(t *testing.T, f *finder, q request) any {
	t.Helper()
	v, err := f.handle(q)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestLibraryAndExport(t *testing.T) {
	f, _ := fixture(t)
	root := ask(t, f, request{Action: "list", Place: "library"}).(listing)
	if root.Total != 1 || root.Items[0].Name != "Recipes" {
		t.Fatal(root)
	}
	q := request{Action: "list", Place: "library", Query: "PASTRY"}
	r := ask(t, f, q).(listing)
	if r.Total != 1 || r.Items[0].Location != "My files / Recipes" || r.Items[0].Kind != "PDF" {
		t.Fatal(r)
	}
	q.Action = "info"
	q.Path = lostID
	if _, err := f.handle(q); err == nil {
		t.Fatal("trashed descendant visible")
	}
	q.Path = docID
	q.Offset = 1
	d := ask(t, f, q).(detail)
	if d.Images != 2 || !strings.HasSuffix(d.Image, "/b.png") || !strings.Contains(d.Note, "Page 2") || !d.Exportable {
		t.Fatal(d)
	}
	q.Action = "export"
	dest := ask(t, f, q).(string)
	got, err := f.fs.ReadFile(strings.TrimPrefix(dest, "/"))
	if err != nil || string(got) != "%PDF-1.7" {
		t.Fatal(dest, err)
	}
	if _, err = f.handle(q); err == nil {
		t.Fatal("overwrote export")
	}
}

func TestFilesAndPagination(t *testing.T) {
	f, put := fixture(t)
	q := request{Action: "list", Place: "vaults", Query: "café"}
	r := ask(t, f, q).(listing)
	if r.Total != 1 || !strings.Contains(r.Items[0].Path, "Notes/") {
		t.Fatal(r)
	}
	q.Hidden = true
	if ask(t, f, q).(listing).Total != 2 {
		t.Fatal("hidden search")
	}
	q.Action = "info"
	q.Path = r.Items[0].Path
	d := ask(t, f, q).(detail)
	if d.Preview != "text" || d.Text != "☕\n" {
		t.Fatal(d)
	}
	put("var/log/example", "hello")
	q.Place = "device"
	q.Path = "."
	q.Action = "list"
	q.Query = "example"
	put("proc/example", "system")
	r = ask(t, f, q).(listing)
	if r.Total != 1 || r.Items[0].Location != "/var/log" {
		t.Fatal(r)
	}
	q.Action = "info"
	q.Path = "var/log/example"
	if ask(t, f, q).(detail).Text != "hello" {
		t.Fatal("device preview")
	}
	put("var/log/binary", "\x00\x01")
	q.Path = "var/log/binary"
	if ask(t, f, q).(detail).Preview != "info" {
		t.Fatal("binary preview")
	}
	q.Path = "../escape"
	if _, err := f.handle(q); err == nil {
		t.Fatal("root escape")
	}
	items := make([]entry, 35)
	for i := range items {
		items[i] = entry{Name: "item", Modified: int64(i)}
	}
	p := paginate(listing{Items: items}, request{Sort: "recent", Offset: 64})
	if len(p.Items) != 3 || p.Items[0].Modified != 2 || p.Offset != 32 {
		t.Fatal(p)
	}
}
