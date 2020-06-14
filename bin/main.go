// Binary eridanus runs eridanus.

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
	"github.com/nullseed/logruseq"
	"github.com/scytrin/eridanus"
	"github.com/sirupsen/logrus"
)

func main() {
	persistPath := flag.String("persist", `Z:\EridanusStore`, "")
	flag.Parse()

	// logrus.SetLevel(logrus.DebugLevel)
	logrus.SetReportCaller(true)
	logrus.SetFormatter(&EFormatter{})
	logrus.AddHook(logruseq.NewSeqHook("http://localhost:5341"))
	log := logrus.StandardLogger()
	ctx := ctxlogrus.ToContext(context.Background(), logrus.NewEntry(log))

	e, err := eridanus.New(ctx, *persistPath)
	if err != nil {
		log.Fatal(err)
	}

	test(ctx, e)

	if err := e.Close(); err != nil {
		log.Fatal(err)
	}

	log.Info("DONE!!!!!")
}

func test(ctx context.Context, e *eridanus.Eridanus) {
	log := ctxlogrus.Extract(ctx)
	for _, us := range []string{
		// "https://pictures.hentai-foundry.com/f/Felox08/792226/Felox08-792226-Snowflake_-_Re_design.jpg",
		"https://www.hentai-foundry.com/pictures/user/Felox08/792226/Snowflake---Re-design",
		"https://www.hentai-foundry.com/pictures/user/Felox08/798105/Singularity",
		// "https://www.hentai-foundry.com/pictures/user/Felox08",
		// "https://www.hentai-foundry.com/user/Felox08/profile",
	} {
		u, err := url.Parse(us)
		if err != nil {
			log.Error(err)
			continue
		}
		r, err := e.Get(ctx, u)
		if err != nil {
			log.Error(err)
			continue
		}
		log.Debugf("%s\n%s", u, strings.Join(r.Format(), "\n"))
	}
}

// EFormatter is an implementation of a logrus.Formatter for use with eridanus.
type EFormatter struct{ logrus.TextFormatter }

// Format formats a logrus.Entry instance.
func (f *EFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	fmt.Fprintf(buf, "[%s] %s:%d -- %s\n",
		entry.Time.Format("2006-01-02 15:04:05.00"),
		entry.Caller.File,
		entry.Caller.Line,
		entry.Level)

	var keys []string
	for k := range entry.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(buf, "  %s = %v\n", k, entry.Data[k])
	}

	fmt.Fprintln(buf, entry.Message)
	return buf.Bytes(), nil
}
