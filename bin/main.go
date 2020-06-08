// Binary eridanus runs eridanus.

//BREAK go:generate rsrc -manifest main.manifest -o rsrc.syso
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"sort"

	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
	"github.com/nullseed/logruseq"
	"github.com/scytrin/eridanus"
	"github.com/sirupsen/logrus"
)

var (
	persistDirDefault = `Z:\EridanusStore`
	cfg               = eridanus.Config{}
)

func init() {
	flag.StringVar(&cfg.LocalStorePath, "persist", persistDirDefault, "")
}

func main() {
	flag.Parse()

	// logrus.SetLevel(logrus.DebugLevel)
	logrus.SetReportCaller(true)
	logrus.SetFormatter(&EFormatter{})
	logrus.AddHook(logruseq.NewSeqHook("http://localhost:5341"))
	log := logrus.StandardLogger()
	ctx := ctxlogrus.ToContext(context.Background(), logrus.NewEntry(log))

	if err := eridanus.Run(ctx, cfg); err != nil {
		log.Fatal(err)
	}

	log.Info("DONE!!!!!")
}

// EFormatter is an implementation of a logrus.Formatter for use with eridanus.
type EFormatter struct{ logrus.TextFormatter }

// Format formats a logrus.Entry instance.
func (f *EFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	fmt.Fprintf(buf, "%s %s -- %s\n",
		entry.Time.Format("2006-01-02 15:04:05.00"),
		entry.Level.String(),
		entry.Message,
	)
	if entry.Caller != nil {
		fmt.Fprintf(buf, "  logCall = %s:%d\n",
			entry.Caller.File, entry.Caller.Line)
	}
	var keys []string
	for k := range entry.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(buf, "  %s = %v\n", k, entry.Data[k])
	}
	return buf.Bytes(), nil
}
