// Binary eridanus runs eridanus.

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/scytrin/eridanus"
	"golang.org/x/sys/windows/registry"
)

const (
	defaultExtensionID = "chjkejdbkhankpkdbblplenaicliflpd"
	contentType        = "application/json"
)

var (
	client          = &http.Client{Timeout: 5 * time.Second}
	msgSizeBytesLen = binary.Size(uint32(0))
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	install := flag.Bool("install", false, "")
	extID := flag.String("ext_id", defaultExtensionID, "")
	appPort := flag.Int("port", 39485, "")
	flag.Parse()

	if *install {
		if err := setupRegistryChromeNMH(ctx, *extID); err != nil {
			log.Fatal(err)
		}
		return
	}

	if err := run(os.Stdin, os.Stdout, fmt.Sprintf("http://localhost:%d", *appPort)); err != nil {
		log.Fatal(err)
	}
}

func run(r io.Reader, w io.Writer, appURL string) error {
	for {
		cmds, err := Get(r)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		log.Println(cmds)

		results := make([]*eridanus.Command, len(cmds.GetCommands()))
		for i, cmd := range cmds.GetCommands() {
			reply, err := callApp(appURL, cmd)
			if err != nil {
				results[i] = &eridanus.Command{Cmd: "error", Data: []string{err.Error()}}
				continue
			}
			results[i] = reply
		}
		out := &eridanus.Commands{Commands: results}
		log.Println(out)

		if err := Put(w, out); err != nil {
			return err
		}
	}
}

func callApp(appURL string, cmd *eridanus.Command) (*eridanus.Command, error) {
	buf := bytes.NewBuffer(nil)
	if err := json.NewEncoder(buf).Encode(cmd); err != nil {
		return nil, err
	}

	res, err := client.Post(appURL, contentType, buf)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var out *eridanus.Command
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get reads a message from the provided io.Reader.
func Get(r io.Reader) (*eridanus.Commands, error) {
	if r == nil {
		return nil, eridanus.ErrNilReader
	}
	length := uint32(0)
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	payload := make([]byte, length)
	if _, err := r.Read(payload); err != nil {
		return nil, err
	}
	cmds := &eridanus.Commands{}
	if err := json.Unmarshal(payload, cmds); err != nil {
		return nil, err
	}
	return cmds, nil
}

// Put writes a message to the provided io.Writer.
func Put(w io.Writer, cmd *eridanus.Commands) error {
	if w == nil {
		return eridanus.ErrNilWriter
	}

	if cmd == nil {
		return eridanus.ErrNilCommand
	}

	out, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(out))); err != nil {
		return err
	}
	if _, err := w.Write(out); err != nil {
		return err
	}
	return nil
}

func setupRegistryChromeNMH(ctx context.Context, extIDs ...string) error {
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
	log.Println(string(nmhManifest))
	log.Println(nmhManifestPath)

	regKey, _, err := registry.CreateKey(registry.CURRENT_USER, keyName, registry.WRITE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %v", err)
	}

	log.Println("writing NMH info to registry")
	if err := regKey.SetStringValue("", nmhManifestPath); err != nil {
		return err
	}

	return regKey.Close()
}
