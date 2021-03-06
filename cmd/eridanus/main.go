// Binary eridanus runs eridanus.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"

	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
	"github.com/nullseed/logruseq"
	"github.com/scytrin/eridanus"
	"github.com/scytrin/eridanus/fetcher"
	"github.com/scytrin/eridanus/storage"
	"github.com/scytrin/eridanus/storage/backend/diskv"
	"github.com/sirupsen/logrus"
)

// https://gitgud.io/prkc/hydrus-companion - Hydrus Companion, a browser extension for hydrus.
// https://gitgud.io/prkc/dolphin-hydrus-actions - Adds Hydrus right-click context menu actions to Dolphin file manager.
// https://gitgud.io/koto/hydrus-dd - DeepDanbooru neural network tagging for Hydrus
// https://gitgud.io/koto/hydrus-archive-delete - Archive/Delete filter in your web browser
// https://gitlab.com/cryzed/hydrus-api - A python module that talks to the API.
// https://github.com/cravxx/hydrus.js - A node.js module that talks to the API.

const defaultExtensionID = "chjkejdbkhankpkdbblplenaicliflpd"

func init() {
	logrus.SetFormatter(&logrus.TextFormatter{ForceQuote: true, QuoteEmptyFields: true})
	// logrus.SetFormatter(&logrus.JSONFormatter{PrettyPrint: true, DataKey: "data"})
	logrus.SetLevel(logrus.InfoLevel)
	logrus.SetReportCaller(true)
	logrus.AddHook(logruseq.NewSeqHook("http://localhost:5341"))
	log.SetOutput(logrus.StandardLogger().Writer())
}

func main() {
	log := logrus.StandardLogger()
	defer log.Info("DONE!!!!!")

	ctx, cancel := context.WithCancel(ctxlogrus.ToContext(context.Background(), logrus.NewEntry(log)))
	logrus.DeferExitHandler(cancel)

	go func() {
		c := make(chan os.Signal)
		defer close(c)
		signal.Notify(c, os.Interrupt, os.Kill)
		defer signal.Stop(c)
		log.Infof("got signal: %v", <-c)
		log.Exit(0)
	}()

	persistPath := flag.String("persist", `Z:\EridanusStore`, "")
	appPort := flag.Int("port", 39485, "")
	flag.Parse()

	sbe := diskv.NewBackend(*persistPath)
	logrus.DeferExitHandler(func() {
		if err := sbe.Close(); err != nil {
			log.Error(err)
		}
	})

	s := storage.NewStorage(sbe)
	f, err := fetcher.NewFetcher(s)
	if err != nil {
		log.Fatal(err)
	}
	logrus.DeferExitHandler(func() {
		if err := f.Close(); err != nil {
			log.Error(err)
		}
	})

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *appPort),
		Handler: http.HandlerFunc(jsonCmdHandler),
	}
	logrus.DeferExitHandler(func() {
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Error(err)
		}
	})

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error(err)
	}
}

func jsonCmdHandler(w http.ResponseWriter, r *http.Request) {
	log := logrus.StandardLogger()
	defer r.Body.Close()

	var req interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error(err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Info(req)

	var res interface{}
	res = &eridanus.Command{Cmd: "okay"} // do stuff
	if err := json.NewEncoder(w).Encode(res); err != nil {
		log.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
