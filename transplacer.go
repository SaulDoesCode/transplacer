package transplacer

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"net/textproto"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/cespare/xxhash"
	"github.com/cornelk/hashmap"
	"github.com/fsnotify/fsnotify"
	"golang.org/x/net/html"
)

var (
	// Compressable - list of compressable file types, append to it if needed
	Compressable = []string{"", ".txt", ".htm", ".html", ".css", ".toml", ".php", ".js", ".json", ".md", ".mdown", ".xml", ".svg", ".go", ".cgi", ".py", ".pl", ".aspx", ".asp"}
	// AvoidPushing - list of file extentions to skip over when building an http2 push list
	AvoidPushing = []string{"", ".png", ".webp", ".txt"}
)

// HashMap is an alias of cornelk/hashmap
type HashMap = hashmap.HashMap

// AssetCache is a store for assets
type AssetCache struct {
	Dir string

	Index   string
	NoIndex bool

	Cache *HashMap

	Expire   time.Duration
	Interval time.Duration

	CacheControl string

	Ticker *time.Ticker

	DevMode bool

	Watch   bool
	Watcher *fsnotify.Watcher

	NotFoundHandler func(http.ResponseWriter, *http.Request)
	NotFoundError   error
}

// Make prepares a new *AssetCache for use
func Make(a *AssetCache) (*AssetCache, error) {
	dir, err := filepath.Abs(a.Dir)
	if err != nil {
		return nil, err
	}
	a.Dir = dir

	if a.Index == "" {
		a.Index = PrepPath(a.Dir, "index.html")
	}

	if a.Cache == nil {
		a.Cache = hashmap.New(50)
	}

	if a.CacheControl == "" {
		a.CacheControl = "private, must-revalidate"
	}

	if a.NotFoundError == nil {
		a.NotFoundError = ErrAssetNotFound
	}

	if a.NotFoundHandler == nil {
		a.NotFoundHandler = func(res http.ResponseWriter, req *http.Request) {
			res.WriteHeader(404)
			res.Write([]byte(a.NotFoundError.Error()))
		}
	}

	if a.Interval == 0 {
		a.Interval = time.Second * 30
	}

	a.SetExpiryCheckInterval(a.Interval)

	if a.Watch {
		a.Watcher, err = fsnotify.NewWatcher()
		if err != nil {
			panic(fmt.Errorf(
				"AssetCache: failed to build file watcher: %v",
				err,
			))
		}
		go func() {
			for {
				select {
				case e := <-a.Watcher.Events:
					if a.DevMode {
						fmt.Printf(
							"\nAssetCache watcher event:\n\tfile: %s \n\t event %s\n",
							e.Name,
							e.Op.String(),
						)
					}

					if !a.Update(e.Name) && a.DevMode {
						fmt.Println("AssetCache error: changed file could not be updated sucessfully")
					}
				case err := <-a.Watcher.Errors:
					fmt.Println("AssetCache file watcher error: ", err)
				}
			}
		}()
	}

	return a, err
}

// SetExpiryCheckInterval generates a new ticker with a set interval
func (a *AssetCache) SetExpiryCheckInterval(interval time.Duration) {
	if a.Ticker != nil {
		a.Ticker.Stop()
	}
	a.Interval = interval
	a.Ticker = time.NewTicker(interval)
	go func() {
		for now := range a.Ticker.C {
			for kv := range a.Cache.Iter() {
				asset := kv.Value.(*Asset)
				if asset.Loaded.Add(a.Expire).Before(now) {
					key := kv.Key.(string)
					a.Del(key)
					if a.DevMode {
						fmt.Println("Asset Expired: ", key)
					}
				}
			}
		}
	}()
}

// StopExpiryCheckInterval stops asset expiration checks
func (a AssetCache) StopExpiryCheckInterval() {
	a.Ticker.Stop()
	a.Ticker = nil
}

// Close stops and clears the AssetCache
func (a *AssetCache) Close() error {
	a.Cache = &HashMap{}
	if a.Ticker != nil {
		a.Ticker.Stop()
	}
	return nil
}

// Gen generates a new Asset
func (a *AssetCache) Gen(name string) (*Asset, error) {
	name = PrepPath(a.Dir, name)

	fs, err := os.Stat(name)
	if err != nil {
		return nil, err
	}

	if fs.IsDir() {
		return a.Gen(filepath.Join(name, "index.html"))
	}

	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	content, err := ioutil.ReadFile(name)
	if err != nil {
		return nil, err
	}

	ext := filepath.Ext(name)

	ContentType := mime.TypeByExtension(ext)

	Compressed := StringsContainCI(Compressable, ext)

	asset := &Asset{
		ContentType:  ContentType,
		Content:      bytes.NewReader(content),
		Compressed:   Compressed,
		CacheControl: a.CacheControl,
		Cache:        a,
		ModTime:      fs.ModTime(),
		Ext:          ext,
		Name:         fs.Name(),
	}

	if Compressed {
		compressed, err := gzipBytes(content, 9)
		if err != nil {
			return nil, err
		}
		compressedReader := bytes.NewReader(compressed)
		var et []byte
		h := xxhash.New()
		_, err = io.Copy(h, compressedReader)
		if err != nil {
			return nil, err
		}
		if et == nil {
			et = h.Sum(nil)
		}
		asset.EtagCompressed = fmt.Sprintf(`"%x"`, base64.StdEncoding.EncodeToString(et))
		asset.ContentCompressed = compressedReader
	}

	var et []byte
	h := xxhash.New()
	_, err = io.Copy(h, f)
	if err != nil {
		return nil, err
	}
	if et == nil {
		et = h.Sum(nil)
	}
	asset.Etag = fmt.Sprintf(`"%x"`, base64.StdEncoding.EncodeToString(et))

	if err != nil {
		return nil, err
	}
	asset.Loaded = time.Now()
	if ext == ".html" {
		list, err := queryPushables(asset.Content)
		if err == nil {
			asset.PushList = list
			if a.DevMode {
				fmt.Println(name, "HTTP2 Push List:\n\t", list)
			}
		}
	}

	if a.Watch {
		a.Watcher.Add(name)
	}
	a.Cache.Set(name, asset)
	return asset, err
}

// Get fetches an asset
func (a *AssetCache) Get(name string) (*Asset, bool) {
	name = PrepPath(a.Dir, name)

	raw, ok := a.Cache.GetStringKey(name)
	if !ok {
		asset, err := a.Gen(name)
		if err != nil && a.DevMode {
			fmt.Println("AssetCache.Get err: ", err, "name: ", name)
		}
		return asset, err == nil && asset != nil
	}
	return raw.(*Asset), ok
}

// Del removes an asset, nb. not the file, the file is fine
func (a *AssetCache) Del(name string) {
	name = PrepPath(a.Dir, name)
	a.Cache.Del(name)
	if a.Watch && a.Watcher != nil {
		a.Watcher.Remove(name)
	}
}

// Update first deletes an asset then gets it again, updating it thereby.
func (a *AssetCache) Update(name string) bool {
	name = PrepPath(a.Dir, name)
	a.Cache.Del(name)
	_, ok := a.Get(name)
	return ok
}

// ErrAssetNotFound is for when an asset cannot be located/created
var ErrAssetNotFound = errors.New(`no asset/file found, cannot serve`)

// ServeFileDirect takes a key/filename directly and serves it if it exists and returns an ErrAssetNotFound if it doesn't
func (a *AssetCache) ServeFileDirect(res http.ResponseWriter, req *http.Request, file string) error {
	asset, ok := a.Get(file)
	if !ok {
		return a.NotFoundError
	}
	asset.Serve(res, req)
	return nil
}

// ServeFile parses a key/filename and serves it if it exists and returns an ErrAssetNotFound if it doesn't
func (a *AssetCache) ServeFile(res http.ResponseWriter, req *http.Request, file string) error {
	return a.ServeFileDirect(res, req, PrepPath(a.Dir, file))
}

// Middleware is a generic go handler that sets up AssetCache like any other
// static file serving solution on your server
func (a *AssetCache) Middleware(h http.HandlerFunc) http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		if req.Method[0] != 'G' {
			h(res, req)
			return
		}

		var err error
		if req.URL.Path == "/" && !a.NoIndex {
			err = a.ServeFileDirect(res, req, a.Index)
		} else {
			err = a.ServeFile(res, req, req.URL.Path)
		}

		if err != nil && a.NotFoundHandler != nil {
			a.NotFoundHandler(res, req)
		}
	}
}

// ServeHTTP implements the http.Handler interface
func (a *AssetCache) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	if req.Method[0] != 'G' {
		return
	}

	var err error
	if req.URL.Path == "/" && !a.NoIndex {
		err = a.ServeFileDirect(res, req, a.Index)
	} else {
		err = a.ServeFile(res, req, req.URL.Path)
	}

	if err != nil && a.NotFoundHandler != nil {
		a.NotFoundHandler(res, req)
	}
}

// Serve is the same as ServeHTTP but it returns the error instead of
// calling .NotFoundHandler, this is useful for echo/air middleware
func (a *AssetCache) Serve(res http.ResponseWriter, req *http.Request) error {
	if req.URL.Path == "/" && !a.NoIndex {
		return a.ServeFileDirect(res, req, a.Index)
	}
	return a.ServeFile(res, req, req.URL.Path)
}

// Asset is an http servable resource
type Asset struct {
	Ext string

	Name string

	ContentType string

	Loaded time.Time

	ModTime time.Time

	Content           *bytes.Reader
	ContentCompressed *bytes.Reader

	CacheControl string

	Cache *AssetCache

	Etag           string
	EtagCompressed string

	Compressed bool

	PushList []string
}

// Serve serves the asset via the ussual http ResponseWriter and *Request
func (as *Asset) Serve(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", as.ContentType)
	if res.Header().Get("Cache-Control") == "" {
		res.Header().Set("Cache-Control", as.CacheControl)
	}

	if req.TLS != nil {
		if len(as.PushList) > 0 {
			pushWithHeaders(res, req, as.PushList)
			if as.Cache != nil && as.Cache.DevMode {
				fmt.Println("http2 push")
			}
		}
	}

	if as.Compressed && strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") {
		res.Header().Set("Etag", as.EtagCompressed)
		res.Header().Set("Content-Encoding", "gzip")
		res.Header().Set("Vary", "accept-encoding")
		http.ServeContent(res, req, as.Name, as.ModTime, as.ContentCompressed)
	} else {
		res.Header().Set("Etag", as.Etag)
		http.ServeContent(res, req, as.Name, as.ModTime, as.Content)
	}
	as.Loaded = time.Now()
}

func gzipBytes(content []byte, level int) ([]byte, error) {
	var b bytes.Buffer

	gz, err := gzip.NewWriterLevel(&b, level)
	if err != nil {
		return nil, err
	}

	if _, err := gz.Write(content); err != nil {
		return nil, err
	}
	if err := gz.Flush(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// PrepPath joins a host path with a clean file path
func PrepPath(host, file string) string {
	file = path.Clean(file)

	if !strings.HasPrefix(file, host) {
		file = filepath.Join(host, file)
	}

	if file[len(file)-1] == '/' {
		return filepath.Join(file, "index.html")
	} else if filepath.Ext(file) == "" {
		return file + ".html"
	}
	return file
}

// HTTP2Push initiates an HTTP/2 server push. This constructs a synthetic request
// using the target and headers, serializes that request into a PUSH_PROMISE
// frame, then dispatches that request using the server's request handlec. The
// target must either be an absolute path (like "/path") or an absolute URL
// that contains a valid host and the same scheme as the parent request. If the
// target is a path, it will inherit the scheme and host of the parent request.
// The headers specifies additional promised request headers. The headers
// cannot include HTTP/2 pseudo headers like ":path" and ":scheme", which
// will be added automatically.
func HTTP2Push(W http.ResponseWriter, target string, headers http.Header) error {
	p, ok := W.(http.Pusher)
	if !ok {
		return nil
	}

	var pos *http.PushOptions
	if l := len(headers); l > 0 {
		pos = &http.PushOptions{
			Header: make(http.Header, l),
		}

		pos.Header.Set("cache-control", "private, must-revalidate")

		for name, values := range headers {
			pos.Header[textproto.CanonicalMIMEHeaderKey(name)] = values
		}
	}

	return p.Push(target, pos)
}

func cloneHeader(h http.Header) http.Header {
	h2 := make(http.Header, len(h))
	for k, vv := range h {
		vv2 := make([]string, len(vv))
		copy(vv2, vv)
		h2[k] = vv2
	}
	return h2
}

func pushWithHeaders(W http.ResponseWriter, R *http.Request, list []string) {
	reqHeaders := cloneHeader(R.Header)
	reqHeaders.Del("etag")
	for name := range reqHeaders {
		if strings.Contains(name, "if") ||
			strings.Contains(name, "modified") {
			reqHeaders.Del(name)
		}
	}
	for _, target := range list {
		HTTP2Push(W, target, reqHeaders)
	}
}

func queryPushables(content io.Reader) ([]string, error) {
	tree, err := html.Parse(content)
	if err != nil {
		return nil, err
	}

	list := []string{}
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode {
			avoid, target := false, ""
			switch n.Data {
			case "link":
				relChecked := false
			LinkLoop:
				for _, a := range n.Attr {
					switch strings.ToLower(a.Key) {
					case "rel":
						if v := strings.ToLower(
							a.Val,
						); v == "preload" ||
							v == "icon" {
							avoid = true
							break LinkLoop
						}

						relChecked = true
					case "href":
						target = a.Val
						if relChecked {
							break LinkLoop
						}
					}
				}
			case "img", "script":
			ImgScriptLoop:
				for _, a := range n.Attr {
					switch strings.ToLower(a.Key) {
					case "src":
						target = a.Val
						break ImgScriptLoop
					}
				}
			}

			for _, nopush := range AvoidPushing {
				if filepath.Ext(target) == nopush {
					avoid = true
					break
				}
			}
			if !avoid && path.IsAbs(target) {
				list = append(list, target)
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}

	f(tree)

	return list, err
}

// StringsContainCI reports whether the lists contains a match regardless of its case.
func StringsContainCI(list []string, match string) bool {
	match = strings.ToLower(match)
	for _, item := range list {
		if strings.ToLower(item) == match {
			return true
		}
	}

	return false
}
