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
	endianess       = binary.LittleEndian
	msgSizeBytesLen = binary.Size(uint32(0))
)

type message struct {
	Commands []*eridanus.Command `json:",omitempty"`
}

func (m message) MarshalBinary() ([]byte, error) {
	w := bytes.NewBuffer(nil)
	out, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	if err := binary.Write(w, endianess, uint32(len(out))); err != nil {
		return nil, err
	}
	if _, err := w.Write(out); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

func (m *message) UnmarshalBinary(data []byte) error {
	r := bytes.NewReader(data)
	length := uint32(0)
	if err := binary.Read(r, endianess, &length); err != nil {
		return err
	}
	payload := make([]byte, length)
	if _, err := r.Read(payload); err != nil {
		return err
	}
	return json.Unmarshal(payload, m)
}

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

	srvAddr := fmt.Sprintf("http://localhost:%d", *appPort)
	if err := run(ctx, os.Stdin, os.Stdout, srvAddr); err != nil && err != io.EOF {
		log.Fatal(err)
	}
}

func run(ctx context.Context, r io.Reader, w io.Writer, appURL string) error {
	for {
		// get query length
		qLen := uint32(0)
		if err := binary.Read(r, binary.LittleEndian, &qLen); err != nil {
			return err
		}
		// forward the query to the main app
		res, err := client.Post(appURL, contentType, io.LimitReader(r, int64(qLen)))
		if err != nil {
			return err
		}
		defer res.Body.Close()
		// extract the reply
		reply, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return err
		}
		// send back to extension
		rLen := uint32(len(reply))
		if err := binary.Write(w, binary.LittleEndian, rLen); err != nil {
			return err
		}
		if _, err := w.Write(reply); err != nil {
			return err
		}
	}
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
