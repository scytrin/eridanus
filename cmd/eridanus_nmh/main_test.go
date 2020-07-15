package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

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

func TestPut(t *testing.T) {
	for _, test := range []struct {
		label   string
		w       io.Writer
		cmd     *eridanus.Commands
		wantErr error
	}{
		{"nil args",
			nil,
			nil,
			eridanus.ErrNilWriter},
		{"nil writer",
			nil,
			&eridanus.Commands{},
			eridanus.ErrNilWriter},
		{"nil command",
			bytes.NewBuffer(nil),
			nil,
			eridanus.ErrNilCommand},
		{"fail on write 0",
			&failOnWrite{0},
			&eridanus.Commands{},
			io.ErrShortWrite},
		{"fail on write 1",
			&failOnWrite{1},
			&eridanus.Commands{},
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
			eridanus.ErrNilReader},
		{"empty reader",
			bytes.NewBuffer(nil),
			io.EOF},
		{"bad json",
			strings.NewReader("falskjdflsdkjsldkjfsk"),
			errors.New("invalid character 'k' looking for beginning of value")},
		{"blank",
			bytes.NewReader(append([]byte{15, 0, 0, 0}, `{"commands":[]}`...)),
			nil},
		{"wrong size",
			bytes.NewReader(append([]byte{82, 0, 0, 0}, `{"commands":[]}`...)),
			errors.New("invalid character '\\x00' after top-level value")},
	} {
		t.Run(test.label, func(t *testing.T) {
			_, err := Get(test.r)
			if err != nil && test.wantErr == nil {
				t.Errorf("Get(...): got %v, want %v", err, test.wantErr)
			}
			if test.wantErr != nil && err.Error() != test.wantErr.Error() {
				t.Errorf("Get(...): got %q, want %q", err, test.wantErr)
			}
		})
	}
}
