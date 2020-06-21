// Binary eridanus runs eridanus.

package main

import (
	"context"
	"flag"
	"log"

	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
	"github.com/nullseed/logruseq"
	"github.com/scytrin/eridanus/fetcher"
	"github.com/scytrin/eridanus/storage"
	"github.com/sirupsen/logrus"
)

func init() {
	logrus.SetFormatter(&logrus.TextFormatter{ForceQuote: true, QuoteEmptyFields: true})
	logrus.SetLevel(logrus.InfoLevel)
	logrus.SetReportCaller(true)
	logrus.AddHook(logruseq.NewSeqHook("http://localhost:5341"))
	log.SetOutput(logrus.StandardLogger().Writer())
}

func main() {
	log := logrus.StandardLogger()
	defer log.Info("DONE!!!!!")

	ctx := ctxlogrus.ToContext(context.Background(), logrus.NewEntry(log))

	persistPath := flag.String("persist", `Z:\EridanusStore`, "")
	flag.Parse()

	s, err := storage.NewStorage(*persistPath)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			log.Error(err)
		}
	}()

	f, err := fetcher.NewFetcher(ctx, s)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Error(err)
		}
	}()

	urls := []string{
		"https://pictures.hentai-foundry.com/f/Felox08/792226/Felox08-792226-Snowflake_-_Re_design.jpg",
		"https://www.hentai-foundry.com/pictures/user/Felox08/792226/Snowflake---Re-design",
		"https://www.hentai-foundry.com/pictures/user/Felox08/798105/Singularity",
		"https://www.hentai-foundry.com/pictures/user/Felox08",
		"https://www.hentai-foundry.com/user/Felox08/profile",
	}
	for _, u := range urls {
		results, err := f.Get(ctx, u)
		if err != nil {
			log.WithField("u", u).Error(err)
			continue
		}
		log.Debug(results)
	}
}
