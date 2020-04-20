package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path"
	"runtime"
	"syscall"

	log "github.com/sirupsen/logrus"
	"stadik.net/eridanus/server"
)

var (
	importsDir, persistFile string
)

func init() {
	log.SetFormatter(new(EFormatter))
	log.SetReportCaller(true)

	flag.StringVar(&importsDir, "imports_dir",
		``,
		// `Z:\Hydrus Network\db\client_files\f*`,
		// `Z:\Hydrus Network\db\client_files\f0b\0b086ce28284e7f94119b569fc40ea1ee5777e2f38ffeb26fa6b12e4a7936b75.jpg`,
		"Directory at which stored media can be found.")
	flag.StringVar(&persistFile, "persist",
		`C:\Users\scytr\Documents\EridanusCache`,
		"")
	flag.Parse()
}

func init() {
	runtime.GOMAXPROCS(runtime.NumCPU())
}

func main() {
	eridanusServer := &server.Server{}
	if err := eridanusServer.Load(persistFile); err != nil {
		log.Error(err)
	}

	httpServer := &http.Server{}
	httpServer = &http.Server{
		Addr:    fmt.Sprintf("localhost:%d", eridanusServer.Port),
		Handler: eridanusServer,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go onSignal(ctx, func() {
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Error(err)
		}
	}, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if importsDir != "" {
			if err := eridanusServer.ImportDir(ctx, importsDir, 10); err != nil {
				log.Error(err)
			}
		}
	}()

	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}

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
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer cancel()
		for sig := range sigChan {
			log.Infof("got signal %v", sig)
			do()
		}
	}()
	<-ctx.Done()
	return ctx.Err()
}
