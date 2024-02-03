package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/waeldot/sealed-secrets-addon/internal/addon"

	flag "github.com/spf13/pflag"
)

// when SIGTERM received,
// this handler notifies goroutines for graceful shutdown.
func handleSigterm(cancel context.CancelFunc) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	<-signals

	log.Printf("received SIGTERM. terminating...")
	cancel()
}

// main function executes 3 loops below,
// 1. http server for probing
// 2. SIGTERM handler
// 3. sealed-secret-addon main loop
func main() {
	http := addon.RunHTTPServer()
	ctx, cancel := context.WithCancel(context.Background())
	go handleSigterm(cancel)
	addon := addon.NewAddon(flag.CommandLine)
	addon.Run(ctx)

	http.Shutdown(ctx)
	log.Println("terminated http server.")
}
