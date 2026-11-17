package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	docsDir   = "home/root/.local/share/remarkable/xochitl"
	vaultDir  = "home/root/reMarkdown"
	pageSize  = 32
	textLimit = 20 * 1024
)

type finder struct {
	fs          *os.Root
	catalog     *catalog
	catalogTime time.Time
}

type entry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Folder   bool   `json:"folder"`
	Modified int64  `json:"modified"`
	Size     int64  `json:"size"`
	Location string `json:"location,omitempty"`
}

type listing struct {
	Items    []entry `json:"items"`
	Title    string  `json:"title"`
	Location string  `json:"location"`
	Total    int     `json:"total"`
	Offset   int     `json:"offset"`
	Warning  string  `json:"warning,omitempty"`
}

type detail struct {
	entry
	Text       string `json:"text,omitempty"`
	Image      string `json:"image,omitempty"`
	Note       string `json:"note,omitempty"`
	Preview    string `json:"preview"`
	Pages      int    `json:"pages,omitempty"`
	ImageIndex int    `json:"imageIndex"`
	Images     int    `json:"images"`
	Exportable bool   `json:"exportable"`
}

func (f *finder) handle(q request) (any, error) {
	if q.Offset < 0 {
		return nil, errors.New("Invalid page.")
	}
	if q.Place == "library" {
		return f.library(q)
	}
	base := "."
	switch q.Place {
	case "device":
	case "vaults":
		base = vaultDir
	default:
		return nil, errors.New("Unknown location.")
	}
	if q.Path == "" {
		q.Path = "."
	}
	if !fs.ValidPath(q.Path) {
		return nil, errors.New("Invalid path.")
	}
	root, err := f.fs.OpenRoot(base)
	if err != nil {
		return nil, fmt.Errorf("Cannot open this location: %w", err)
	}
	defer root.Close()
	if q.Action == "list" {
		return diskList(root, q)
	}
	if q.Action != "info" {
		return nil, errors.New("Unknown action.")
	}
	info, err := root.Lstat(q.Path)
	if err != nil {
		return nil, err
	}
	item := diskEntry(info, q.Path)
	d := detail{entry: item, Preview: "info", Note: "File details"}
	d.Location = "/" + strings.TrimPrefix(path.Join(base, q.Path), "./")
	if !info.Mode().IsRegular() || specialPath(d.Location) {
		return d, nil
	}
	ext := strings.ToLower(path.Ext(q.Path))
	if ext == ".png" || ext == ".jpg" || ext == ".jpeg" {
		d.Image = f.fileURL(path.Join(base, q.Path))
		d.Preview = "image"
		return d, nil
	}
	file, err := root.Open(q.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	b, err := io.ReadAll(io.LimitReader(file, textLimit+1))
	if err != nil {
		return nil, err
	}
	truncated := len(b) > textLimit
	if truncated {
		b = b[:textLimit]
		for n := 0; n < 3 && len(b) > 0 && !utf8.Valid(b) && !utf8.RuneStart(b[len(b)-1]); n++ {
			b = b[:len(b)-1]
		}
		if len(b) > 0 && !utf8.Valid(b) && b[len(b)-1] >= 0xc0 {
			b = b[:len(b)-1]
		}
	}
	if utf8.Valid(b) && strings.IndexFunc(string(b), func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) < 0 {
		d.Preview = "text"
		d.Text = string(b)
		d.Note = ""
		if truncated {
			d.Note = "Preview of the first 20 KiB"
		}
	} else {
		d.Note = "A preview is unavailable for this file type."
	}
	return d, nil
}

func (f *finder) fileURL(p string) string {
	u := url.URL{Scheme: "file", Path: filepath.Join(f.fs.Name(), p)}
	return u.String()
}

func specialPath(p string) bool {
	for _, prefix := range []string{"/dev", "/sys", "/proc", "/run"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

func diskEntry(i os.FileInfo, p string) entry {
	kind := "File"
	switch {
	case i.IsDir():
		kind = "Folder"
	case i.Mode()&os.ModeSymlink != 0:
		kind = "Link"
	case !i.Mode().IsRegular():
		kind = "Special file"
	case path.Ext(p) != "":
		kind = strings.ToUpper(strings.TrimPrefix(path.Ext(p), "."))
	}
	return entry{Name: i.Name(), Path: p, Kind: kind, Folder: i.IsDir(), Size: i.Size(), Modified: i.ModTime().UnixMilli()}
}

func diskList(root *os.Root, q request) (listing, error) {
	title := path.Base(q.Path)
	location := "/"
	if q.Place == "vaults" {
		location = "Obsidian"
		if q.Path != "." {
			location += " / " + q.Path
		} else {
			title = "Obsidian"
		}
	} else if q.Path == "." {
		title = "Device files"
	} else {
		location = "/" + q.Path
	}
	list := listing{Title: title, Location: location}
	query := strings.ToLower(strings.TrimSpace(q.Query))
	dir, err := root.Open(q.Path)
	if err != nil {
		return list, err
	}
	defer dir.Close()
	items, err := dir.ReadDir(-1)
	if err != nil {
		return list, err
	}
	add := func(p string, e fs.DirEntry) {
		if !strings.Contains(strings.ToLower(e.Name()), query) {
			return
		}
		info, err := e.Info()
		if err != nil {
			list.Warning = "Some entries could not be read."
			return
		}
		item := diskEntry(info, p)
		if query != "" && path.Dir(p) != q.Path {
			item.Location = path.Dir(p)
			if q.Place == "device" {
				item.Location = "/" + item.Location
			}
		}
		list.Items = append(list.Items, item)
	}
	for _, e := range items {
		if !q.Hidden && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := path.Join(q.Path, e.Name())
		add(p, e)
		// Search ordinary directories without entering kernel-backed pseudo-filesystems.
		if query != "" && e.IsDir() && !(q.Place == "device" && specialPath("/"+p)) {
			err = fs.WalkDir(root.FS(), p, func(s string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					list.Warning = "Some folders could not be read."
					return nil
				}
				if s == p {
					return nil
				}
				if q.Place == "device" && d.IsDir() && specialPath("/"+s) {
					return fs.SkipDir
				}
				if !q.Hidden && strings.HasPrefix(d.Name(), ".") {
					if d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
				add(s, d)
				return nil
			})
			if err != nil {
				return list, err
			}
		}
	}
	return paginate(list, q), nil
}

func paginate(l listing, q request) listing {
	sort.Slice(l.Items, func(i, j int) bool {
		a, b := l.Items[i], l.Items[j]
		if a.Folder != b.Folder {
			return a.Folder
		}
		if q.Sort == "recent" && a.Modified != b.Modified {
			return a.Modified > b.Modified
		}
		an, bn := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if an != bn {
			return an < bn
		}
		return a.Path < b.Path
	})
	l.Total = len(l.Items)
	last := 0
	if l.Total > 0 {
		last = (l.Total - 1) / pageSize * pageSize
	}
	l.Offset = min(q.Offset/pageSize*pageSize, last)
	l.Items = append([]entry{}, l.Items[l.Offset:min(l.Offset+pageSize, l.Total)]...)
	return l
}
