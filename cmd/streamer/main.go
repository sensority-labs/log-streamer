package main

import (
	"log"
	"time"

	"github.com/certifi/gocertifi"
	"github.com/getsentry/sentry-go"

	"github.com/sensority-labs/log-streamer/internal/service"
)

func main() {
	sentryClientOptions := sentry.ClientOptions{
		//Debug:     true,
		Transport: sentry.NewHTTPSyncTransport(),
	}
	rootCAs, err := gocertifi.CACerts()
	if err != nil {
		log.Printf("Could not load CA Certificates: %v\n\n", err)
	} else {
		sentryClientOptions.CaCerts = rootCAs
	}

	if err := sentry.Init(sentryClientOptions); err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}
	// Flush buffered events before the program terminates.
	defer sentry.Flush(2 * time.Second)

	if err := service.Start(); err != nil {
		sentry.CaptureException(err)
		log.Fatalf("Failed to run service: %+v", err)
	}
}
