package main

import (
  "bytes"
  "context"
  "flag"
  "io"
  "io/ioutil"
	"mime/multipart"
	"net/http"
  "os"
  "os/signal"
  "path/filepath"
  "syscall"
  "time"

  "stadik.net/eridanus"
  log "github.com/sirupsen/logrus"
)

var (
  crtFile, keyFile, storageDir, importsDir, persistFile string
  httpServeAddr = "localhost:"+os.Getenv("PORT")
)

func onSignal(ctx context.Context, do func(), signals ...os.Signal) {
  sigChan := make(chan os.Signal, 1)
  defer close(sigChan)
  signal.Notify(sigChan, signals...)
  for {
    select {
    case <-ctx.Done():
      log.Error(ctx.Err())
      return
    case sig, more := <-sigChan:
      log.Infof("got signal %v", sig)
      do()
      if !more {
        return
      }
    }
  }
}
 
func main() {
  log.SetReportCaller(true)
  log.SetFormatter(&log.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})

  flag.StringVar(&crtFile, "crt_file", "", "The TLS crt file")
  flag.StringVar(&keyFile, "key_file", "", "The TLS key file")
  flag.StringVar(&storageDir, "storage_dir", "/home/scytrin/.cache/eridanus/storage", "Directory at which stored media can be found.")
  flag.StringVar(&importsDir, "imports_dir", "/home/scytrin/.cache/eridanus/import", "Directory at which stored media can be found.")
  flag.StringVar(&persistFile, "persist", "/home/scytrin/eridanus_cache", "")
  flag.Parse()
  
  bCtx := context.Background()
  ctx, cancel := context.WithCancel(bCtx)
  go onSignal(ctx, cancel, syscall.SIGINT, syscall.SIGTERM)

	eridanusServer, err := eridanus.NewServer(eridanus.Config{
    StoragePath: storageDir,
  })
	if err != nil {
    log.Fatal(err)
  }

  go onSignal(ctx, func(){
    if err := eridanusServer.BuildPhashStore(); err != nil {
      log.Fatal(err)
    }
  }, syscall.SIGHUP)

  httpServer := &http.Server{
    Addr: httpServeAddr,
    Handler: eridanusServer.NewServeMux(),
  }
  go func() {
    if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
      log.Fatal(err)
    }
  }()

  time.Sleep(5*time.Second)

  if persistFile != "" {
    persistContent, err := ioutil.ReadFile(persistFile)
    if err != nil {
      persistFile = ""
      log.Errorf("failed to load persist: %v", err)
    } else {
      eridanusServer.UnmarshalText(persistContent)
    }
  }

  if err := filepath.Walk(importsDir, uploadWalkFunc); err != nil {
		log.Fatal(err)
  }

  if samesies, err := eridanusServer.FindSimilar(); err != nil {
		log.Fatal(err)
  } else {
    log.Print(samesies)
  }

  <-ctx.Done()

  if persistFile != "" {
    persistContent, err := eridanusServer.MarshalText()
    if err != nil {
      log.Errorf("failed to save persist: %v", err)
    }
    log.Infof("serialized: %s", persistContent) 
    if err := ioutil.WriteFile(persistFile, persistContent, 0644); err != nil {
      log.Errorf("failed to save persist: %v", err)
    }
  }

  httpServer.Shutdown(bCtx)

  log.Info("exited gracefully")
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
