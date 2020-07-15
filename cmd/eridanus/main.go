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
	fmt.Println("hi")
	log.Info(os.Args)

	appPort := flag.Int("port", 39485, "")
	persistPath := flag.String("persist", `Z:\EridanusStore`, "")
	flag.Parse()

	ctx, cancel := context.WithCancel(ctxlogrus.ToContext(context.Background(), logrus.NewEntry(log)))
	defer cancel()

	sbe := diskv.NewBackend(*persistPath)
	defer sbe.Close()

	s, err := storage.NewStorage(sbe)
	if err != nil {
		log.Fatal(err)
	}

	f, err := fetcher.NewFetcher(s)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *appPort),
		Handler: http.HandlerFunc(jsonCmdHandler),
	}
	go func() {
		<-ctx.Done()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Error(err)
		}
		if err := f.Close(); err != nil {
			log.Error(err)
		}
		if err := sbe.Close(); err != nil {
			log.Error(err)
		}
	}()
	log.Fatal(httpServer.ListenAndServe())
}

func jsonCmdHandler(w http.ResponseWriter, r *http.Request) {
	log := logrus.StandardLogger()
	defer r.Body.Close()

	req := &eridanus.Command{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error(err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Info(req)

	res := &eridanus.Command{Cmd: "okay"} // do stuff
	if err := json.NewEncoder(w).Encode(res); err != nil {
		log.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
