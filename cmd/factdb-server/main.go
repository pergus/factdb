package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/pergus/factdb/internal/server"
	"github.com/pergus/factdb/internal/store"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	data := flag.String("data", "./factdb-data.json", "snapshot file path")

	flag.Parse()
	st, err := store.Open(*data)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}

	srv := &http.Server{Addr: *listen, Handler: server.New(st).Handler(), ReadHeaderTimeout: 5 * time.Second}

	log.Printf("FactDB listening on http://%s (snapshot %s)", *listen, *data)
	log.Fatal(srv.ListenAndServe())
}
