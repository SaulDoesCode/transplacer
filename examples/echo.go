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
