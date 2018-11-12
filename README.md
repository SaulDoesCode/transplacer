# transplacer: reads files, suspending them in memory for performant serving/access

[![GoDoc](http://img.shields.io/badge/go-documentation-blue.svg?style=flat-square)](http://godoc.org/github.com/SaulDoesCode/transplacer)

## Diy AssetCache for super fast static asset serving straight from the memory
```go
package main

import (
  "net/http"
  "log"
  "time"

  tr "github.com/SaulDoesCode/transplacer"
)

func main() {
  cache := tr.Make(&tr.AssetCache{
    Dir: "./assets",
    Watch: true,
    Expire: time.Minute * 30,
  })

  server := &http.Server{
    Addr: ":http",
    Handler: cache,
  }

  log.Fatal(server.ListenAndServe())
}
```

With Echo 
```go
package main

import (
  "github.com/labstack/echo"
  tr "github.com/SaulDoesCode/transplacer"
)

func main() {
  cache := tr.Make(&tr.AssetCache{
    Dir: "./assets",
    Watch: true,
    Expire: time.Minute * 30,
  })

  e := echo.New()

  e.Use(func (next echo.HandlerFunc) echo.HandlerFunc {
    return func(c echo.Context) error {
      req := c.Request()
      err := next(c)
      if err == nil || req.Method[0] != 'G' {
        return err
      }

      return cache.Serve(c.Resonse().Writer, req)
    }
  })

  e.Logger.Fatal(e.Start(":http"))
}
```