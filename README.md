[![GoDoc](http://img.shields.io/badge/go-documentation-blue.svg?style=flat-square)](http://godoc.org/github.com/SaulDoesCode/transplacer)

## Features

* auto http2 push, when available
* auto watching and updating
* works concurrently, no deadlocks
* it's a bit of rough magic and ducktape, but, it works


## Diy AssetCache for super fast static asset serving straight from the memory
```go
package main

import (
	"log"
	"net/http"
	"time"

	tr "github.com/SaulDoesCode/transplacer"
)

func main() {
	cache, err := tr.Make(&tr.AssetCache{
		Dir:     "./assets",
		Watch:   true,
		Expire:  time.Minute * 30,
		DevMode: true, // extra logs
	})
	if err != nil {
		panic(err.Error())
	}

	server := &http.Server{
		Addr:    ":http",
		Handler: cache,
	}

	log.Fatal(server.ListenAndServe())
}
```

With Echo 
```go
package main

import (
	"time"

	tr "github.com/SaulDoesCode/transplacer"
	"github.com/labstack/echo"
)

func main() {
	cache, err := tr.Make(&tr.AssetCache{
		Dir:    "./assets",
		Watch:  true,
		Expire: time.Minute * 30,
	})
	if err != nil {
		panic(err.Error())
	}

	e := echo.New()

	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			err := next(c)
			if err == nil || req.Method[0] != 'G' {
				return err
			}

			return cache.Serve(c.Response().Writer, req)
		}
	})

	e.Logger.Fatal(e.Start(":http"))
}
```