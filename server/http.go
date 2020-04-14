package server

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
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
	serveMux.HandleFunc("/upload", s.uploadHandler)

	if s.cfg.StoragePath != "" {
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
	log.Info(r)
	dirName, pageName := path.Split(r.URL.Path)

	// index page
	if dirName != "/" || pageName == "" {
		fmt.Fprintf(w, "%#v", r)
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

	if r.Method == http.MethodGet {
		fmt.Fprint(w, `<html><body><form method="post" enctype="multipart/form-data">
<input type="file" name="files" multiple="multiple" />
<input type="submit" />
</form></body></html>`)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	fmt.Fprintln(w, "<html><body>")
	fmt.Fprintln(w, "<h1>Uploaded files</h1>")

	mr, err := r.MultipartReader()
	if err != nil {
		log.Errorf("file upload err (reader): %v", err)
		return
	}

	for {
		hash, err := s.handleNextPart(mr)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Error(err)
			break
		}
		fmt.Fprintf(w, "<div><a href='/image/%[1]s'>%[1]s</a></div>\n", hash)
	}

	fmt.Fprintln(w, "</body></html>")
}

func (s *Server) handleNextPart(mr *multipart.Reader) (string, error) {
	part, err := mr.NextPart()
	if err == io.EOF {
		return "", err
	}
	if err != nil {
		log.Errorf("file upload err (part): %v", err)
		return "", err
	}
	defer part.Close()

	if part.FormName() != "files" {
		return "", nil
	}

	return s.ingestFile(part.FileName(), part)
}

func (s *Server) ingestFile(name string, r io.Reader) (string, error) {
	fileBytes, err := ioutil.ReadAll(r)
	if err != nil {
		log.Errorf("file upload err (read): %v", err)
		return "", err
	}

	fileHash, err := s.buildHash(bytes.NewReader(fileBytes))
	if err != nil {
		log.Errorf("file upload err (ihash): %v", err)
		return "", err
	}

	tags := []string{fmt.Sprintf("_:%s", fileHash)}

	if name != "" {
		tags = append(tags, fmt.Sprintf("filename:%s", name))
	}

	percHash, err := s.buildPHash(bytes.NewReader(fileBytes))
	if err != nil {
		log.Warnf("%s upload err (phash): %v", fileHash, err)
	}
	if percHash > 0 {
		tags = append(tags, fmt.Sprintf("phash:%x", percHash))
	}

	if err := s.AddTags(fileHash, tags...); err != nil {
		log.Errorf("%s upload err (tags): %v", fileHash, err)
		return "", err
	}

	filePath := filepath.Join(s.cfg.StoragePath, fileHash)
	if err := ioutil.WriteFile(filePath, fileBytes, 0644); err != nil {
		log.Errorf("%s upload err (write): %v", fileHash, err)
		return "", err
	}

	return fileHash, nil
}
