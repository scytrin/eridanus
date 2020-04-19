package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path"
	"syscall"

	log "github.com/sirupsen/logrus"
	"stadik.net/eridanus/server"
)

var (
	crtFile, keyFile, storageDir, importsDir, persistFile string
	httpServeAddr                                         string
	port                                                  int
)

func init() {
	flag.IntVar(&port, "port", 8080, "")
	// flag.StringVar(&crtFile, "crt_file", "", "The TLS crt file")
	// flag.StringVar(&keyFile, "key_file", "", "The TLS key file")
	flag.StringVar(&storageDir, "storage_dir",
		`Z:\Hydrus Network\db\client_files\`,
		"Directory at which stored media can be found.")
	flag.StringVar(&importsDir, "imports_dir",
		``,
		// `Z:\Hydrus Network\db\client_files\f*`,
		// `Z:\Hydrus Network\db\client_files\f0b\0b086ce28284e7f94119b569fc40ea1ee5777e2f38ffeb26fa6b12e4a7936b75.jpg`,
		"Directory at which stored media can be found.")
	flag.StringVar(&persistFile, "persist",
		`C:\Users\scytr\Documents\EridanusCache`,
		"")
	flag.Parse()

	log.SetFormatter(new(EFormatter))
	log.SetReportCaller(true)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Info("initializing eridanus server")
	eridanusServer := &server.Server{}
	if err := eridanusServer.Load(persistFile); err != nil {
		log.Error(err)
	}
	eridanusServer.StoragePath = storageDir

	log.Info("initializing http server")
	httpServeAddr = fmt.Sprintf("localhost:%d", port)
	httpServer := &http.Server{
		Addr:    httpServeAddr,
		Handler: eridanusServer,
	}

	go onSignal(ctx, func() {
		defer cancel()
		if err := httpServer.Close(); err != nil {
			log.Error(err)
		}
	}, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if importsDir != "" {
			if err := eridanusServer.ImportDir(ctx, importsDir, 10, false); err != nil {
				log.Error(err)
			}
		}
	}()

	log.Info("serving...")
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}

	log.Print("shutting down")
	if err := eridanusServer.Save(persistFile); err != nil {
		log.Error(err)
	}

	log.Print("exited gracefully")
}

type EFormatter struct{ log.TextFormatter }

func (f *EFormatter) Format(entry *log.Entry) ([]byte, error) {
	return []byte(fmt.Sprintf(
		"%s %s %s:%d -- %s\n",
		entry.Time.Format(f.TimestampFormat),
		entry.Level.String(),
		path.Base(entry.Caller.File),
		entry.Caller.Line,
		entry.Message,
	)), nil
}

func onSignal(ctx context.Context, do func(), signals ...os.Signal) error {
	sigChan := make(chan os.Signal, 1)
	defer close(sigChan)
	signal.Notify(sigChan, signals...)
	defer signal.Stop(sigChan)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sig, more := <-sigChan:
			log.Infof("got signal %v", sig)
			do()
			if !more {
				return nil
			}
		}
	}
}
