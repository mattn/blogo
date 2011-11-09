package main

import (
	"bytes"
	"errors"
)
import "html"
import "io"
import "io/ioutil"
import "github.com/hoisie/mustache.go/mustache"
import "os"
import "path/filepath"
import "regexp"
import "strings"
import "time"
import "web"
import "encoding/json"

type Tag struct {
	Name string
}

type Entry struct {
	Id       string
	Filename string
	Title    string
	Body     string
	Created  *time.Time
	Category string
	Author   string
	Tags     []Tag
}

func toTextChild(w io.Writer, n *html.Node) error {
	switch n.Type {
	case html.ErrorNode:
		return errors.New("unexpected ErrorNode")
	case html.DocumentNode:
		return errors.New("unexpected DocumentNode")
	case html.ElementNode:
	case html.TextNode:
		w.Write([]byte(n.Data))
	case html.CommentNode:
		return errors.New("COMMENT")
	default:
		return errors.New("unknown node type")
	}
	for _, c := range n.Child {
		if err := toTextChild(w, c); err != nil {
			return err
		}
	}
	return nil
}

func toText(n *html.Node) (string, error) {
	if n == nil || len(n.Child) == 0 {
		return "", nil
	}
	b := bytes.NewBuffer(nil)
	for _, child := range n.Child {
		if err := toTextChild(b, child); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

func GetEntry(filename string) (entry *Entry, err error) {
	fi, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	in_body := false
	re, err := regexp.Compile("^meta-([a-zA-Z]+):[:space:]*(.*)$")
	if err != nil {
		return nil, err
	}
	for n, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if n == 0 {
			entry = new(Entry)
			entry.Title = line
			entry.Filename = filepath.Clean(filename)
			entry.Tags = []Tag{}
			entry.Created = time.SecondsToUTC(fi.Ctime_ns / 1e9)
			continue
		}
		if n > 0 && len(line) == 0 {
			in_body = true
			continue
		}
		if in_body == false && re.MatchString(line) {
			submatch := re.FindStringSubmatch(line)
			if submatch[1] == "tags" {
				tags := strings.Split(submatch[2], ",")
				entry.Tags = make([]Tag, len(tags))
				for i, t := range tags {
					entry.Tags[i].Name = strings.TrimSpace(t)
				}
			}
			if submatch[1] == "author" {
				entry.Author = submatch[2]
			}
		} else {
			entry.Body += strings.Trim(line, "\r") + "\n"
		}
	}
	if entry == nil {
		err = errors.New("invalid entry file")
	}
	return
}

type Entries []*Entry

func (p *Entries) VisitDir(path string, f *os.FileInfo) bool { return true }
func (p *Entries) VisitFile(path string, f *os.FileInfo) {
	if strings.ToLower(filepath.Ext(path)) != ".txt" {
		return
	}
	if entry, err := GetEntry(path); err == nil {
		*p = append(*p, entry)
	}
}

func GetEntries(path string, useSummary bool) (entries *Entries, err error) {
	entries = new(Entries)
	e := make(chan error)
	filepath.Walk(path, entries, e)
	for _, entry := range *entries {
		if useSummary {
			doc, err := html.Parse(strings.NewReader(entry.Body))
			if err == nil {
				if text, err := toText(doc); err == nil {
					if len(text) > 500 {
						text = text[0:500] + "..."
					}
					entry.Body = text
				}
			}
		}
		entry.Id = entry.Filename[len(path):len(entry.Filename)-3] + "html"
	}
	if len(*entries) == 0 {
		entries = nil
	}
	return
}

type Config map[string]interface{}

func (c *Config) Set(key string, val interface{}) {
	(*c)[key] = val
}

func (c *Config) Is(key string) bool {
	val, ok := (*c)[key].(bool)
	if !ok {
		return false
	}
	return val
}

func (c *Config) Get(key string) string {
	val, ok := (*c)[key].(string)
	if !ok {
		return ""
	}
	return val
}

func LoadConfig() (config *Config) {
	root, _ := filepath.Split(filepath.Clean(os.Args[0]))
	b, err := ioutil.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		println(err.Error())
		return &Config{}
	}
	err = json.Unmarshal(b, &config)
	return
}

func Render(ctx *web.Context, tmpl string, config *Config, name string, data interface{}) {
	tmpl = filepath.Join(config.Get("datadir"), tmpl)
	ctx.WriteString(mustache.RenderFile(tmpl,
		map[string]interface{}{
			"config": config,
			name:     data}))
}

func main() {
	config := LoadConfig()
	web.Get("/(.*)", func(ctx *web.Context, path string) {
		config = LoadConfig()
		datadir := config.Get("datadir")
		if path == "" || path[len(path)-1] == '/' {
			dir := filepath.Join(datadir, path)
			stat, err := os.Stat(dir)
			if err != nil || !stat.IsDirectory() {
				ctx.NotFound("File Not Found")
				return
			}
			entries, err := GetEntries(dir, config.Is("useSummary"))
			if err == nil {
				Render(ctx, "entries.mustache", config, "entries", entries)
				return
			}
		} else if len(path) > 5 && path[len(path)-5:] == ".html" {
			file := filepath.Join(datadir, path[:len(path)-5]+".txt")
			_, err := os.Stat(file)
			if err != nil {
				ctx.NotFound("File Not Found" + err.Error())
				return
			}
			entry, err := GetEntry(file)
			if err == nil {
				Render(ctx, "entry.mustache", config, "entry", entry)
				return
			}
		}
		ctx.Abort(500, "Server Error")
	})
	//web.Config.RecoverPanic = false
	web.Config.StaticDir = config.Get("staticdir")
	web.Run(config.Get("host"))
}
