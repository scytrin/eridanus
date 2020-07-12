package nmh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/scytrin/eridanus"
)

type failOnWrite struct{ count int }

func (f *failOnWrite) Write(b []byte) (int, error) {
	if f.count <= 0 {
		return 0, io.ErrShortWrite
	}
	f.count--
	return len(b), nil
}

func TestNMH(t *testing.T) {
	for i, test := range []struct {
		cmd  string
		data []string
		err  error
	}{
		{"hello", []string{"world"}, errors.New("expected error")},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			r := bytes.NewBuffer(nil)
			w := bytes.NewBuffer(nil)
			// preload a message
			if err := Put(r, &eridanus.Command{Cmd: test.cmd, Data: test.data}); err != nil {
				t.Fatal(err)
			}
			if err := Run(r, w, func(cmd *eridanus.Command, send Sender) error {
				if cmd.Cmd != test.cmd {
					t.Errorf("cmd mismatch: got %s, want %s", cmd.Cmd, test.cmd)
				}
				if diff := cmp.Diff(cmd.Data, test.data); diff != "" {
					t.Errorf("data mismatch diff: %s", diff)
				}
				return send(cmd)
			}); err != nil {
				t.Fatal(err)
			}
		})
	}

	t.Run("handler error", func(t *testing.T) {
		herr := errors.New("faaaail")
		r := bytes.NewBuffer(nil)
		w := bytes.NewBuffer(nil)
		// preload a message
		if err := Put(r, &eridanus.Command{}); err != nil {
			t.Fatal(err)
		}
		if err := Run(r, w, func(cmd *eridanus.Command, send Sender) error {
			return herr
		}); err != herr {
			t.Errorf("Run(...): got %v, want %v", err, herr)
		}
	})
}

func TestPut(t *testing.T) {
	for _, test := range []struct {
		label   string
		w       io.Writer
		cmd     *eridanus.Command
		wantErr error
	}{
		{"nil args",
			nil,
			nil,
			ErrNilWriter},
		{"nil writer",
			nil,
			&eridanus.Command{},
			ErrNilWriter},
		{"nil command",
			bytes.NewBuffer(nil),
			nil,
			ErrNilCommand},
		{"fail on write 0",
			&failOnWrite{0},
			&eridanus.Command{},
			io.ErrShortWrite},
		{"fail on write 1",
			&failOnWrite{1},
			&eridanus.Command{},
			io.ErrShortWrite},
	} {
		t.Run(test.label, func(t *testing.T) {
			if err := Put(test.w, test.cmd); err != test.wantErr {
				t.Errorf("Put(...): got %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestGet(t *testing.T) {
	for _, test := range []struct {
		label   string
		r       io.Reader
		wantErr error
	}{
		{"nil reader",
			nil,
			ErrNilReader},
		{"empty reader",
			bytes.NewBuffer(nil),
			io.EOF},
		{"bad json",
			strings.NewReader("fa;lskjdf;lsdkjf;sldkjf;sk"),
			&json.MarshalerError{}},
	} {
		t.Run(test.label, func(t *testing.T) {
			if _, err := Get(test.r); err != test.wantErr {
				t.Errorf("Get(...): got %v, want %v", err, test.wantErr)
			}
		})
	}
}
