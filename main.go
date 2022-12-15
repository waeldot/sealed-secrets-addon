package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	flag "github.com/spf13/pflag"

	"platform/k8s/sealed-secrets-addon/pkg/addon"
	"platform/k8s/sealed-secrets-addon/pkg/buildinfo"
)

// set addon version from Makefile, default: UNKNOWN
var (
	VERSION = buildinfo.DefaultVersion
)

// main function executes below loops,
// 1. http server for probe
// 2. sigterm handler
// 3. sealed-secret-addon main loop
func main() {
	buildinfo.FallbackVersion(&VERSION, buildinfo.DefaultVersion)
	log.Printf("sealed-secrets-addon version: %s", VERSION)

	http := addon.RunHTTPServer()
	ctx, cancel := context.WithCancel(context.Background())
	go handleSigterm(cancel)
	addon := addon.NewAddon(flag.CommandLine)
	addon.Run(ctx)

	http.Shutdown(ctx)
	log.Println("terminated http server.")
}

// when SIGTERM received,
// this handler notify goroutines for graceful shutdown.
func handleSigterm(cancel context.CancelFunc) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	<-signals

	log.Printf("received SIGTERM. terminating...")
	cancel()
}
