package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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
		`C:\Users\scytr\Documents\EridanusStore`,
		"Directory at which stored media can be found.")
	flag.StringVar(&importsDir, "imports_dir",
		"",
		"Directory at which stored media can be found.")
	flag.StringVar(&persistFile, "persist",
		`C:\Users\scytr\Documents\EridanusCache`,
		"")
}

func main() {
	flag.Parse()

	log.SetReportCaller(true)
	log.SetFormatter(&log.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})

	eridanusServer, err := server.NewServer(server.Config{
		StoragePath: storageDir,
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := eridanusServer.Load(persistFile); err != nil {
		log.Error(err)
	}

	httpServeAddr = fmt.Sprintf("localhost:%d", port)
	httpServer := &http.Server{
		Addr:    httpServeAddr,
		Handler: eridanusServer.NewServeMux(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go onSignal(ctx, func() {
		defer cancel()
		log.Info(httpServer.Close())
	}, syscall.SIGINT, syscall.SIGTERM)

	go onSignal(ctx, func() {
		if err := eridanusServer.BuildPhashStore(); err != nil {
			log.Fatal(err)
		}
	}, syscall.SIGHUP)

	go func() {
		time.Sleep(5 * time.Second)
		if importsDir != "" {
			if err := filepath.Walk(importsDir, uploadWalkFunc); err != nil {
				log.Error(err)
			}
		}
		if samesies, err := eridanusServer.FindSimilar(); err != nil {
			log.Error(err)
		} else {
			log.Print(samesies)
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

func uploadFileBody(path string) (io.Reader, string, error) {
	body := bytes.NewBuffer(nil)
	writer := multipart.NewWriter(body)
	defer writer.Close()

	part, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return nil, "", err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	if _, err := io.Copy(part, file); err != nil {
		return nil, "", err
	}

	return body, writer.FormDataContentType(), nil
}

func uploadFileRequest(url, path string) (*http.Request, error) {
	body, contentType, err := uploadFileBody(path)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)
	return req, nil
}

func uploadWalkFunc(path string, info os.FileInfo, walkErr error) error {
	if walkErr != nil {
		log.Warn(walkErr)
		return nil
	}
	if info.IsDir() {
		return nil
	}
	req, err := uploadFileRequest("http://"+httpServeAddr+"/upload", path)
	if err != nil {
		return err
	}
	_, err = (&http.Client{}).Do(req)
	return err
}
