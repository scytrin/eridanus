package eridanus

import (
  "fmt"
  "io/ioutil"
	"net/http"
	"path"
  "path/filepath"
  
  log "github.com/sirupsen/logrus"
)

const (
	imagePagePath = "/image/"
)

func (s *Server) NewServeMux() *http.ServeMux {
  serveMux := http.NewServeMux()
  serveMux.Handle("/", s)
  
  if s.cfg.StoragePath != "" {
    serveMux.HandleFunc("/upload", s.uploadHandler)
    
    serveMux.Handle(imagePagePath,
      http.StripPrefix(imagePagePath,
        http.FileServer(
          http.Dir(s.cfg.StoragePath))))
  }
  
  //Leaving in place as this looks to be a really clean way to support GRPC
  // if s.cfg.GRPCServer != nil {
  //   for serviceName := range s.cfg.GRPCServer.GetServiceInfo() {
  //     serveMux.Handle(fmt.Sprintf("/%s/", serviceName), s.cfg.GRPCServer)
  //   }
  // }

  return serveMux
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
  dirName, pageName := path.Split(r.URL.Path)

	if dirName != "/" || pageName == "" {
	  http.NotFound(w, r)
    return
  }

  tags, err := s.GetTags(pageName)
  if err != nil {
    log.Print(err)
	  http.NotFound(w, r)
    return
  }

  if tags == nil {
	  http.NotFound(w, r)
    return
  }

  fmt.Fprintf(w, "%#v", tags)
}

// https://zupzup.org/go-http-file-upload-download/
func (s *Server) uploadHandler(w http.ResponseWriter, r *http.Request) {
  if s.cfg.StoragePath == "" {
    http.Error(w, "no configured storage path", http.StatusPreconditionFailed)
    return
  }

  file, handler, err := r.FormFile("file")
  if err != nil {
    log.Errorf("file upload err (form): %v", err)
    return
  }
  defer file.Close()

  fileBytes, err := ioutil.ReadAll(file)
  if err != nil {
    log.Errorf("file upload err (read): %v", err)
    return
  }
  
  fileHash, err := s.buildHash(fileBytes)
  if err != nil {
    log.Errorf("file upload err (ihash): %v", err)
    return
  }

  tags := []string{
    fmt.Sprintf("_:%s", fileHash),
    fmt.Sprintf("filename:%s", handler.Filename),
  }

  percHash, err := s.buildPHash(fileBytes)
  if err != nil {
    log.Warnf("%s upload err (phash): %v", fileHash, err)
  }
  if percHash > 0 {
    tags = append(tags, fmt.Sprintf("phash:%x", percHash))
  }

  if err := s.AddTags(fileHash, tags...); err != nil {
    log.Errorf("%s upload err (tags): %v", fileHash, err)
    return
  }

  filePath := filepath.Join(s.cfg.StoragePath, fileHash)
  if err := ioutil.WriteFile(filePath, fileBytes, 0644); err != nil {
    log.Errorf("%s upload err (write): %v", fileHash, err)
    return
  }

  fmt.Fprintf(w, "Uploaded file:\n%#v\n", fileHash)
}
