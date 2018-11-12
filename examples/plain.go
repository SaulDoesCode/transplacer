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
