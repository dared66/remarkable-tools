package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var uuid = regexp.MustCompile("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")

type metadata struct {
	VisibleName  string          `json:"visibleName"`
	Parent       string          `json:"parent"`
	Type         string          `json:"type"`
	Deleted      bool            `json:"deleted"`
	LastModified json.RawMessage `json:"lastModified"`
}

type doc struct {
	entry
	parent   string
	original string
}

type catalog struct {
	byID     map[string]*doc
	children map[string][]*doc
	warning  string
}

func (f *finder) scan() (*catalog, error) {
	dir, err := f.fs.Open(docsDir)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	files, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	c := &catalog{byID: map[string]*doc{}, children: map[string][]*doc{}}
	names := make(map[string]bool, len(files))
	for _, file := range files {
		names[file.Name()] = file.Type().IsRegular()
	}
	for _, file := range files {
		id, ok := strings.CutSuffix(file.Name(), ".metadata")
		if !ok || !uuid.MatchString(id) || !file.Type().IsRegular() {
			continue
		}
		var m metadata
		if err := f.readJSON(file.Name(), &m, 256*1024); err != nil {
			c.warning = "Some document metadata could not be read."
			continue
		}
		if m.Deleted || m.Parent == "trash" {
			continue
		}
		kind := "Notebook"
		original := ""
		switch m.Type {
		case "CollectionType":
			kind = "Folder"
		case "DocumentType":
			for _, ext := range []string{".epub", ".pdf"} {
				if names[id+ext] {
					original = id + ext
					kind = strings.ToUpper(ext[1:])
					break
				}
			}
		default:
			continue
		}
		modified, _ := strconv.ParseInt(strings.Trim(string(m.LastModified), "\""), 10, 64)
		name := m.VisibleName
		if name == "" {
			name = "Untitled"
		}
		d := &doc{entry: entry{Name: name, Path: id, Kind: kind, Folder: kind == "Folder", Modified: modified}, parent: m.Parent, original: original}
		c.byID[id] = d
		c.children[m.Parent] = append(c.children[m.Parent], d)
	}
	return c, nil
}

func (f *finder) readJSON(file string, value any, limit int64) error {
	in, err := f.fs.Open(path.Join(docsDir, file))
	if err != nil {
		return err
	}
	defer in.Close()
	return json.NewDecoder(io.LimitReader(in, limit)).Decode(value)
}

// Only items reachable from My files are visible. This also excludes descendants
// of trashed folders, orphaned records and corrupt parent cycles.
func (c *catalog) location(id string) (string, error) {
	parts := []string{}
	seen := map[string]bool{}
	for id != "" {
		d := c.byID[id]
		if d == nil || seen[id] {
			return "", errors.New("This item is no longer in My files. Refresh the list.")
		}
		seen[id] = true
		parts = append(parts, d.Name)
		id = d.parent
	}
	result := "My files"
	for i := len(parts) - 1; i >= 0; i-- {
		result += " / " + parts[i]
	}
	return result, nil
}

func (f *finder) library(q request) (any, error) {
	if f.catalog == nil || q.Refresh || time.Since(f.catalogTime) > time.Minute {
		c, err := f.scan()
		if err != nil {
			return nil, fmt.Errorf("Cannot read My files: %w", err)
		}
		f.catalog = c
		f.catalogTime = time.Now()
	}
	c := f.catalog
	location, err := c.location(q.Path)
	if err != nil {
		return nil, err
	}
	item := c.byID[q.Path]
	switch q.Action {
	case "list":
		title := "My files"
		if item != nil {
			if !item.Folder {
				return nil, errors.New("This item is a document.")
			}
			title = item.Name
		}
		list := listing{Title: title, Location: location, Warning: c.warning}
		query := strings.ToLower(strings.TrimSpace(q.Query))
		pending := []string{q.Path}
		for len(pending) > 0 {
			parent := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			for _, d := range c.children[parent] {
				if query != "" && d.Folder {
					pending = append(pending, d.Path)
				}
				if !strings.Contains(strings.ToLower(d.Name), query) {
					continue
				}
				e := d.entry
				if query != "" {
					e.Location, _ = c.location(d.parent)
				}
				list.Items = append(list.Items, e)
			}
		}
		return paginate(list, q), nil
	case "info", "export":
		if item == nil || item.Folder {
			return nil, errors.New("Select a document.")
		}
		if q.Action == "export" {
			return f.export(item)
		}
		return f.documentInfo(item, location, q.Offset)
	default:
		return nil, errors.New("Unknown action.")
	}
}

func (f *finder) documentInfo(item *doc, location string, index int) (detail, error) {
	d := detail{entry: item.entry, Preview: "info", Note: "No cached page preview. Open the document in the tablet’s My files to read or write.", Exportable: item.original != ""}
	d.Location = location
	var content struct {
		FileType  string   `json:"fileType"`
		PageCount int      `json:"pageCount"`
		Pages     []string `json:"pages"`
		CPages    struct {
			Pages []struct {
				ID  string `json:"id"`
				Idx struct {
					Value string `json:"value"`
				} `json:"idx"`
				Deleted struct {
					Value bool `json:"value"`
				} `json:"deleted"`
			} `json:"pages"`
		} `json:"cPages"`
	}
	_ = f.readJSON(item.Path+".content", &content, 8*1024*1024)
	d.Pages = content.PageCount
	switch strings.ToLower(content.FileType) {
	case "pdf":
		d.Kind = "PDF"
	case "epub":
		d.Kind = "EPUB"
	}
	if item.original != "" {
		if info, err := f.fs.Stat(path.Join(docsDir, item.original)); err == nil {
			d.Size = info.Size()
		}
	}
	dirname := path.Join(docsDir, item.Path+".thumbnails")
	dir, err := f.fs.Open(dirname)
	if err != nil {
		if os.IsNotExist(err) {
			return d, nil
		}
		return d, err
	}
	defer dir.Close()
	files, err := dir.ReadDir(-1)
	if err != nil {
		return d, err
	}
	images := map[string]string{}
	for _, file := range files {
		ext := strings.ToLower(path.Ext(file.Name()))
		if file.Type().IsRegular() && (ext == ".png" || ext == ".jpg" || ext == ".jpeg") {
			images[strings.TrimSuffix(file.Name(), path.Ext(file.Name()))] = file.Name()
		}
	}
	pages := content.Pages
	if len(content.CPages.Pages) > 0 {
		pages = nil
		sort.SliceStable(content.CPages.Pages, func(i, j int) bool { return content.CPages.Pages[i].Idx.Value < content.CPages.Pages[j].Idx.Value })
		for _, p := range content.CPages.Pages {
			if !p.Deleted.Value {
				pages = append(pages, p.ID)
			}
		}
	}
	if d.Pages == 0 {
		d.Pages = len(pages)
	}
	type thumbnail struct {
		name   string
		number int
	}
	var ordered []thumbnail
	for i, id := range pages {
		if name, ok := images[id]; ok {
			ordered = append(ordered, thumbnail{name, i + 1})
		}
	}
	if len(pages) == 0 {
		for _, name := range images {
			ordered = append(ordered, thumbnail{name, 0})
		}
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].name < ordered[j].name })
	}
	if len(ordered) > 0 {
		d.Images = len(ordered)
		d.ImageIndex = min(index, len(ordered)-1)
		t := ordered[d.ImageIndex]
		d.Preview = "image"
		d.Image = f.fileURL(path.Join(dirname, t.name))
		d.Note = "Cached page preview"
		if t.number > 0 {
			d.Note = fmt.Sprintf("Cached preview · Page %d", t.number)
		}
	}
	return d, nil
}

const exportDir = "home/root/Downloads"

// Publish the completed copy atomically. A duplicate or concurrent export
// cannot replace the first copy or expose a partially copied document.
func (f *finder) export(item *doc) (string, error) {
	if item.original == "" {
		return "", errors.New("This notebook has no original PDF or EPUB to copy.")
	}
	ext := path.Ext(item.original)
	name := strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, item.Name))
	name = strings.TrimSuffix(name, ext)
	if name == "" || strings.HasPrefix(name, ".") {
		name = "Document " + name
	}
	for len(name) > 210 {
		r := []rune(name)
		name = string(r[:len(r)-1])
	}
	final := path.Join(exportDir, name+ext)
	if err := f.fs.MkdirAll(exportDir, 0755); err != nil {
		return "", err
	}
	in, err := f.fs.Open(path.Join(docsDir, item.original))
	if err != nil {
		return "", err
	}
	defer in.Close()
	// Use a rooted temporary name so the export also stays confined in previews.
	tmpName := fmt.Sprintf(".finder-%d-%d.tmp", os.Getpid(), time.Now().UnixNano())
	tmp := path.Join(exportDir, tmpName)
	out, err := f.fs.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer f.fs.Remove(tmp)
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err = f.fs.Link(tmp, final); err != nil {
		if os.IsExist(err) {
			return "", errors.New("A file with this name already exists in Downloads.")
		}
		return "", err
	}
	return "/" + filepath.ToSlash(final), nil
}
