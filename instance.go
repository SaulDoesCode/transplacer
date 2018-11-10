package mak

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/json-iterator/go"
	"golang.org/x/crypto/acme/autocert"
)

// Instance is a mak server with all the trinkets and bells
type Instance struct {
	Server          *http.Server
	SecondaryServer *http.Server

	Config    *Config
	RawConfig map[string]interface{}

	Router *Router

	Middleware    []Middleware
	PreMiddleware []Middleware

	AssetWares []Middleware

	SecondaryServerHandler http.Handler

	AutoCert *autocert.Manager

	ErrorHandler func(*Ctx, error) error

	NotFoundHandler func(*Ctx) error

	AssetCache *AssetCache
}

// AddWare adds middleware(s) to the instance
func (in *Instance) AddWare(wares ...Middleware) {
	in.Middleware = append(in.Middleware, wares...)
}

// AddHTTPWare adds plain http middleware(s) to the instance
func (in *Instance) AddHTTPWare(wares ...func(http.Handler) http.Handler) {
	for _, httpware := range wares {
		in.Middleware = append(in.Middleware, WrapHTTPMiddleware(httpware))
	}
}

// AddPreHTTPWare adds plain http middleware(s) to the instance
func (in *Instance) AddPreHTTPWare(wares ...func(http.Handler) http.Handler) {
	for _, httpware := range wares {
		in.PreMiddleware = append(in.PreMiddleware, WrapHTTPMiddleware(httpware))
	}
}

// AddPreWare adds middleware(s) to the instance,
// wares that run before all the others
func (in *Instance) AddPreWare(wares ...Middleware) {
	in.PreMiddleware = append(in.PreMiddleware, wares...)
}

// Config holds all the information necessary to fire up a mak instance
type Config struct {
	AppName         string `json:"appname,omitempty" toml:"appname,omitempty"`
	Domain          string `json:"domain,omitempty" toml:"domain,omitempty"`
	MaintainerEmail string `json:"maintainer_email,omitempty" toml:"maintainer_email,omitempty"`

	DevMode bool `json:"devmode,omitempty" toml:"devmode,omitempty"`

	Address                string `json:"address" toml:"address"`
	SecondaryServerAddress string `json:"secondary_server_address" toml:"secondary_server_address"`

	DevAddress                string `json:"dev_address,omitempty" toml:"dev_address,omitempty"`
	DevSecondaryServerAddress string `json:"dev_secondary_server_address,omitempty" toml:"dev_secondary_server_address,omitempty"`

	PreferMsgpack bool `json:"prefer_msgpack,omitempty" toml:"prefer_msgpack,omitempty"`

	AutoPush bool `json:"autopush,omitempty" toml:"autopush,omitempty"`

	AutoCert    bool     `json:"autocert,omitempty" toml:"autocert,omitempty"`
	DevAutoCert bool     `json:"dev_autocert,omitempty" toml:"dev_autocert,omitempty"`
	Whitelist   []string `json:"whitelist,omitempty" toml:"whitelist,omitempty"`
	Certs       string   `json:"certs,omitempty" toml:"certs,omitempty"`

	TLSKey  string `json:"tls_key,omitempty" toml:"tls_key,omitempty"`
	TLSCert string `json:"tls_cert,omitempty" toml:"tls_cert,omitempty"`

	Assets           string `json:"assets,omitempty" toml:"assets,omitempty"`
	DoNotWatchAssets bool   `json:"do_not_watch_assets,omitempty" toml:"do_not_watch_assets,omitempty"`

	Private string `json:"private,omitempty" toml:"private,omitempty"`

	Cache string `json:"cache,omitempty" toml:"cache,omitempty"`

	Raw map[string]interface{} `json:"-" toml:"-"`
}

func digestConfig(config *Config) {
	if config.Private == "" {
		config.Private = "./private"
	}

	if config.Assets == "" {
		config.Assets = "./assets"
	}

	if config.Certs == "" {
		config.Certs = config.Private + "/certs"
	}

	if config.Cache == "" {
		config.Cache = config.Private + "/cache"
	}

	if config.AutoCert && len(config.Whitelist) == 0 && config.Domain != "" {
		config.Whitelist = []string{config.Domain}
	}
}

// Make Creates a new mak Instance
func Make(conf *Config) *Instance {
	digestConfig(conf)

	in := &Instance{
		Config: conf,
		Router: MakeRouter(),

		Server: &http.Server{
			Addr: conf.Address,
		},
		SecondaryServer: &http.Server{
			Addr: conf.SecondaryServerAddress,
		},

		RawConfig: conf.Raw,
	}

	return in
}

// ServeHTTP implements the `http.Handler`.
func (in *Instance) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	ctx := &Ctx{
		Status:        200,
		ContentLength: r.ContentLength,
		Path:          r.RequestURI,
		R:             r,
		W:             rw,

		instance:               in,
		parseParamsOnce:        &sync.Once{},
		parseClientAddressOnce: &sync.Once{},
	}

	// Chain Middleware

	h := func(c *Ctx) error {
		rh := in.Router.Route(c)
		h := func(c *Ctx) error {
			if err := rh(c); err != nil {
				return err
			} else if !c.Written {
				return c.WriteContent(nil)
			}
			return nil
		}

		for i := len(in.Middleware) - 1; i >= 0; i-- {
			h = in.Middleware[i](h)
		}

		return h(c)
	}

	// Chain PreMiddleware

	for i := len(in.PreMiddleware) - 1; i >= 0; i-- {
		h = in.PreMiddleware[i](h)
	}

	// Execute all Middleware

	if err := h(ctx); err != nil {
		if in.ErrorHandler != nil {
			in.ErrorHandler(ctx, err)
		}

		if ctx.Written {
			return
		}

		if ctx.Status < 400 {
			ctx.Status = 500
		}

		m := err.Error()
		if ctx.Status == 500 && !in.Config.DevMode {
			m = "internal server error"
		}

		if ctx.R.Method == "GET" || ctx.R.Method == "HEAD" {
			ctx.DelHeader("etag")
			ctx.DelHeader("last-modified")
		}

		ctx.WriteString(m)
	}

	// Close opened request param file values

	for _, p := range ctx.params {
		for _, pv := range p.Values {
			if pv.f != nil && pv.f.f != nil {
				pv.f.f.Close()
			}
		}
	}

	if r.RequestURI == "/" && r.Method == "GET" && in.Config.Assets != "" {
		err := ctx.WriteFile(prepPath(in.Config.Assets, "index.html"))
		if err == nil {
			return
		}
	}
}

// Run let's the mak instance's purpose actuate, until it dies or is otherwise stopped
func (in *Instance) Run() error {
	cf := in.Config

	if cf.DevMode {
		cf.AutoCert = cf.DevAutoCert
		if cf.DevAddress != "" {
			in.Server.Addr = cf.DevAddress
		}
		if cf.DevSecondaryServerAddress != "" {
			in.SecondaryServer.Addr = cf.DevSecondaryServerAddress
		}
	}

	if in.SecondaryServer.Addr == "" {
		in.SecondaryServer = nil
	}

	if cf.Assets != "" {
		assets, err := filepath.Abs(cf.Assets)
		if err != nil {
			fmt.Println("assets dir error, cannot get absolute path: ", err)
			panic("could not get an absolute path for the assets directory")
		}
		cf.Assets = assets

		stat, err := os.Stat(cf.Assets)
		if err != nil {
			fmt.Println("assets dir err: ", err)
			panic("something wrong with the Assets dir/path, best you check what's going on")
		}
		if !stat.IsDir() {
			panic("the path of assets, leads to no folder sir, you best fix that now!")
		}

		ac, err := MakeAssetCache(
			cf.Assets,
			time.Minute*30,
			time.Second*30,
			!cf.DoNotWatchAssets,
		)
		if err != nil {
			fmt.Println("asset cache failure: ", err)
			panic("could not establish an asset cache")
		}
		in.AssetCache = ac
		in.AssetCache.Instance = in

		in.STATIC("/", cf.Assets, in.AssetWares...)
	}

	if cf.Cache != "" {
		cachedir, err := filepath.Abs(cf.Cache)
		if err != nil {
			fmt.Println("cache dir error, cannot get absolute path: ", err)
			panic("could not get an absolute path for the cache directory")
		}
		cf.Cache = cachedir

		stat, err := os.Stat(cf.Cache)
		if err != nil {
			fmt.Println("cache dir err: ", err)
			panic("something wrong with the Assets dir/path, best you check what's going on")
		}
		if !stat.IsDir() {
			panic("the path of cache, leads to no folder sir, you best fix that now!")
		}

		// ... eh now what
	}

	if cf.AutoCert {
		in.AutoCert = &autocert.Manager{
			Prompt: autocert.AcceptTOS,
			Cache:  autocert.DirCache(cf.Certs),
			HostPolicy: func(_ context.Context, h string) error {
				if len(cf.Whitelist) == 0 || stringsContainsCI(cf.Whitelist, h) {
					return nil
				}

				return fmt.Errorf("acme/autocert: host %q not configured in config.Whitelist", h)
			},
			Email: cf.MaintainerEmail,
		}

		in.Server.TLSConfig = in.AutoCert.TLSConfig()

		cf.TLSCert = ""
		cf.TLSKey = ""

		in.SecondaryServer.Handler = in.AutoCert.HTTPHandler(in.SecondaryServerHandler)
	} else {
		if in.SecondaryServerHandler == nil {
			in.SecondaryServer.Handler = http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
				target := "https://"

				host, _, err := net.SplitHostPort(req.Host)
				if err != nil {
					host = req.Host
				}

				target += host + in.Server.Addr + req.URL.Path
				if len(req.URL.RawQuery) > 0 {
					target += "?" + req.URL.RawQuery
				}

				http.Redirect(res, req, target, 301)
			})
		} else {
			in.SecondaryServer.Handler = in.SecondaryServerHandler
		}
	}

	in.Server.Handler = in

	if in.AutoCert != nil || cf.TLSCert != "" {
		go in.SecondaryServer.ListenAndServe()
	}
	err := in.Server.ListenAndServeTLS(cf.TLSCert, cf.TLSKey)
	return err
}

// TimelyStop makes the mak instance run no longer, when specified
func (in *Instance) TimelyStop(when time.Duration, postStop func()) {
	go func() {
		time.Sleep(when)
		in.Server.Close()
		in.SecondaryServer.Close()
		if postStop != nil {
			postStop()
		}
	}()
}

// Stop makes the mak instance run no longer
func (in *Instance) Stop() error {
	in.SecondaryServer.Close()
	return in.Server.Close()
}

// MakeFromConf read's a config file for configuration instructions
// instead of the usual user/manually generated *Config
func MakeFromConf(location string) *Instance {
	raw, err := ioutil.ReadFile(location)
	if err != nil {
		panic("no config file to start a mak instance with")
	}

	var conf Config
	var rawconf map[string]interface{}

	if strings.Contains(location, ".json") {
		err = jsoniter.Unmarshal(raw, &conf)
		if err == nil {
			err = jsoniter.Unmarshal(raw, &rawconf)
		}
	} else if strings.Contains(location, ".toml") {
		err = toml.Unmarshal(raw, &conf)
		if err == nil {
			err = toml.Unmarshal(raw, &rawconf)
		}
	}

	if err != nil {
		fmt.Println("MakeFromConf err: ", err)
		panic("bad config file, it cannot be parsed. make sure it's valid json or toml")
	}

	conf.Raw = rawconf
	return Make(&conf)
}

// stringsContainsCI reports whether the lists contains a match regardless of its case.
func stringsContainsCI(list []string, match string) bool {
	match = strings.ToLower(match)
	for _, item := range list {
		if strings.ToLower(item) == match {
			return true
		}
	}

	return false
}
