package addon

import (
	"io"
	"log"
	"net/http"
	"time"
)

func RunHTTPServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, err := io.WriteString(w, "ok\n")
		if err != nil {
			log.Fatal(err)
		}
	})

	server := http.Server{
		Addr:         ":8081",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	log.Printf("started http server serving on %s", server.Addr)
	go server.ListenAndServe()

	return &server
}
