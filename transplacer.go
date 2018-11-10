package mak

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/cornelk/hashmap"
	"github.com/fsnotify/fsnotify"
)

var (
	// Compressable - list of compressable file types, append to it if needed
	Compressable = []string{"", ".txt", ".htm", ".html", ".css", ".toml", ".php", ".js", ".json", ".md", ".mdown", ".xml", ".svg", ".go", ".cgi", ".py", ".pl", ".aspx", ".asp"}
)

// HashMap is an alias of cornelk/hashmap
type HashMap = hashmap.HashMap

// AssetCache is a store for assets
type AssetCache struct {
	Dir   string
	Cache *HashMap

	Expire   time.Duration
	Interval time.Duration

	CacheControl string

	Ticker *time.Ticker

	Instance *Instance

	Watch   bool
	Watcher *fsnotify.Watcher
}

// MakeAssetCache prepares a new *AssetCache for use
func MakeAssetCache(dir string, expire time.Duration, interval time.Duration, watch bool) (*AssetCache, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	a := &AssetCache{
		Dir:          dir,
		Cache:        &HashMap{},
		Expire:       expire,
		CacheControl: "private, must-revalidate",
		Watch:        watch,
	}

	a.SetExpiryCheckInterval(interval)

	go func() {
		for now := range a.Ticker.C {
			for kv := range a.Cache.Iter() {
				asset := kv.Value.(*Asset)
				if asset.Loaded.Add(a.Expire).After(now) {
					a.Cache.Del(kv.Key)
				}
			}
		}
	}()

	if a.Watch {
		a.Watcher, err = fsnotify.NewWatcher()
		if err != nil {
			panic(fmt.Errorf(
				"air: failed to build coffer watcher: %v",
				err,
			))
		}
		go func() {
			for {
				select {
				case e := <-a.Watcher.Events:
					if a.Instance.Config.DevMode {
						fmt.Printf(
							"\nAssetCache watcher event:\n\tfile: %s \n\t event %s\n",
							e.Name,
							e.Op.String(),
						)
					}

					a.Del(e.Name)
					a.Gen(e.Name)
				case err := <-a.Watcher.Errors:
					fmt.Println("AssetCache watcher error: ", err)
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
}

// Handler serves the assets
func (a *AssetCache) Handler(c *Ctx) error {
	name := path.Clean(a.Dir + c.R.URL.Path)

	asset, ok := a.Get(name)
	if ok {
		return asset.Serve(c)
	}

	err := ErrNotFound.Envoy(c)
	if c.instance.ErrorHandler != nil {
		return c.instance.ErrorHandler(c, err)
	}
	return err
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
	name = prepPath(a.Dir, name)

	fs, err := os.Stat(name)
	if err != nil {
		return nil, err
	}

	if fs.IsDir() {
		return a.Gen(name + "/index.html")
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

	Compressed := stringsContainsCI(Compressable, ext)

	asset := &Asset{
		ContentType:  ContentType,
		Content:      bytes.NewReader(content),
		Compressed:   Compressed,
		CacheControl: a.CacheControl,
		ModTime:      fs.ModTime(),
		Ext:          ext,
	}

	if Compressed {
		compressed, err := gzipBytes(content, 9)
		if err != nil {
			return nil, err
		}
		compressedReader := bytes.NewReader(compressed)
		var et []byte
		h := sha256.New()
		_, err = io.Copy(h, compressedReader)
		if err != nil {
			return nil, err
		}
		if et == nil {
			et = h.Sum(nil)
		}
		asset.EtagCompressed = fmt.Sprintf(`"%x"`, et)
		asset.ContentCompressed = compressedReader
	}

	var et []byte
	h := sha256.New()
	_, err = io.Copy(h, f)
	if err != nil {
		return nil, err
	}
	if et == nil {
		et = h.Sum(nil)
	}
	asset.Etag = fmt.Sprintf(`"%x"`, et)

	if err == nil {
		asset.Loaded = time.Now()
		if ext == ".html" {
			list, err := queryPushables(string(content))
			if err == nil {
				asset.PushList = list
			}
		}

		a.Cache.Set(name, asset)
		if a.Watch {
			a.Watcher.Add(name)
		}
	}

	return asset, err
}

// Get fetches an asset
func (a *AssetCache) Get(name string) (*Asset, bool) {
	name = prepPath(a.Dir, name)

	raw, ok := a.Cache.GetStringKey(name)
	if !ok {
		asset, err := a.Gen(name)
		if err != nil && a.Instance.Config.DevMode {
			fmt.Println("AssetCache.Get err: ", err, "name: ", name)
		}
		return asset, err == nil
	}
	return raw.(*Asset), ok
}

// Del removes an asset, nb. not the file, the file is fine
func (a *AssetCache) Del(name string) {
	name = prepPath(a.Dir, name)
	a.Cache.Del(name)
	if a.Watch {
		a.Watcher.Remove(name)
	}
}

// Asset is an http servable resource
type Asset struct {
	Ext string

	ContentType string

	Loaded time.Time

	ModTime time.Time

	Content           *bytes.Reader
	ContentCompressed *bytes.Reader

	CacheControl string

	Etag           string
	EtagCompressed string

	Compressed bool

	PushList []string
}

// Serve an asset through c *Ctx
func (as *Asset) Serve(c *Ctx) error {
	c.SetContentType(as.ContentType)
	if c.GetHeader("last-modified") == "" {
		c.SetHeader("last-modified", as.ModTime.UTC().Format(http.TimeFormat))
	}

	match := c.Header("If-None-Match")
	if match == "" {
		match = c.Header("If-Match")
	}
	if match != "" {
		if strings.Contains(match, as.Etag) ||
			strings.Contains(match, as.EtagCompressed) {
			return c.WriteContent(nil)
		}
	}

	if c.R.TLS != nil && c.GetHeader("strict-transport-security") == "" {
		c.SetHeader("strict-transport-security", "max-age=31536000")
	}

	c.SetHeader("cache-control", as.CacheControl)
	if len(as.PushList) > 0 {
		pushWithHeaders(c, as.PushList)
	}

	if as.Compressed && strings.Contains(c.Header("accept-encoding"), "gzip") {
		c.SetHeader("etag", as.EtagCompressed)
		c.SetHeader("content-encoding", "gzip")
		c.SetHeader("vary", "accept-encoding")
		http.ServeContent(c.W, c.R, "", as.ModTime, as.Content)
	} else {
		c.SetHeader("etag", as.Etag)
		http.ServeContent(c.W, c.R, "", as.ModTime, as.ContentCompressed)
	}

	c.Written = true

	return nil
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

func prepPath(host, file string) string {
	file = path.Clean(file)

	if !strings.Contains(file, host) {
		file = filepath.Join(host, file)
	}

	if file[len(file)-1] == '/' {
		return filepath.Join(file, "index.html")
	}
	return file
}
