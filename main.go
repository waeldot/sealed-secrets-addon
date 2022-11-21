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

var (
	// set from Makefile, default: UNKNOWN
	VERSION = buildinfo.DefaultVersion
)

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

func handleSigterm(cancel context.CancelFunc) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	<-signals

	log.Printf("received SIGTERM. terminating...")
	cancel()
}
