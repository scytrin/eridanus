// Binary eridanus runs eridanus.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
	"github.com/nullseed/logruseq"
	"github.com/scytrin/eridanus"
	"github.com/scytrin/eridanus/fetcher"
	"github.com/scytrin/eridanus/nmh"
	"github.com/scytrin/eridanus/storage"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows/registry"
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
	log.Info(os.Args)
	defer log.Info("DONE!!!!!")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = ctxlogrus.ToContext(ctx, logrus.NewEntry(log))

	persistPath := flag.String("persist", `Z:\EridanusStore`, "")
	extensionID := flag.String("ext_id", defaultExtensionID, "")
	installOnly := flag.Bool("install", false, "")
	flag.Parse()

	if *installOnly {
		if err := setupRegistryChromeNMH(ctx, *extensionID); err != nil {
			log.Fatal(err)
		}
		return
	}

	s, err := storage.NewStorage(*persistPath)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()

	f, err := fetcher.NewFetcher(s)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	if err := nmh.Run(os.Stdin, os.Stdout, func(cmd *eridanus.Command, send nmh.Sender) error {
		switch cmd.Cmd {
		case "init":
			if err := send(&eridanus.Command{Cmd: "clear"}); err != nil {
				return err
			}

			classNames, err := s.ClassesStorage().Names()
			if err != nil {
				return err
			}

			var classes []*eridanus.URLClass
			for _, name := range classNames {
				cls, err := s.ClassesStorage().Get(name)
				if err != nil {
					return err
				}
				classes = append(classes, cls)
			}

			cmd := &eridanus.Command{Cmd: "classes"}
			for _, uc := range classes {
				cmd.Data = append(cmd.Data, fmt.Sprint(uc.GetName()))
			}

			if err := send(cmd); err != nil {
				return err
			}
		default:
			out := &eridanus.Command{Cmd: "hello"}
			out.Data = append(out.Data, "world")
			return send(out)
		}
		return nil
	}); err != nil {
		log.Fatal(err)
	}
}

type command struct{}

func httpServer(ctx context.Context, cmdChan chan<- command) {
	log := ctxlogrus.Extract(ctx)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	log.Info("Address:", listener.Addr())

	log.Fatal(http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, %q", html.EscapeString(r.URL.Path))
		defer r.Body.Close()
		var cmd command
		if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			log.Error(err)
			return
		}
		cmdChan <- cmd
	})))
}

func setupRegistryChromeNMH(ctx context.Context, extIDs ...string) error {
	log := ctxlogrus.Extract(ctx)

	allowedOrigins := make([]string, len(extIDs))
	for i, id := range extIDs {
		allowedOrigins[i] = fmt.Sprintf("chrome-extension://%s/", id)
	}

	keyName := `Software\Google\Chrome\NativeMessagingHosts\com.github.scytrin.eridanus`
	nmhManifestPath := filepath.Join(filepath.Dir(os.Args[0]), "manifest.json")
	nmhManifest, err := json.Marshal(map[string]interface{}{
		"name":            filepath.Base(keyName),
		"description":     "Eridanus Server Native Messaging Host",
		"path":            "./" + filepath.Base(os.Args[0]),
		"type":            "stdio",
		"allowed_origins": allowedOrigins,
	})
	if err != nil {
		return errors.New("marshalling manifest to json failed")
	}

	if err := ioutil.WriteFile(nmhManifestPath, nmhManifest, 0644); err != nil {
		return err
	}
	log.WithField("at", nmhManifestPath).Debug(string(nmhManifest))

	regKey, _, err := registry.CreateKey(registry.CURRENT_USER, keyName, registry.WRITE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %v", err)
	}

	log.Info("writing NMH info to registry")
	if err := regKey.SetStringValue("", nmhManifestPath); err != nil {
		return err
	}

	return regKey.Close()
}
