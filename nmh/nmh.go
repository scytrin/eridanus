// Package nmh reads and writes as per https://developer.chrome.com/extensions/nativeMessaging
package nmh

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"

	"github.com/scytrin/eridanus"
)

var (
	msgSizeBytesLen = binary.Size(uint32(0))
	// ErrNilCommand is emitted when a nil *eridanis.Command is passed to Put or Run
	ErrNilCommand = errors.New("nil command provided")
	// ErrNilReader is emitted when a nil io.Reader is passed to Get or Run
	ErrNilReader = errors.New("nil reader provided")
	// ErrNilWriter is emitted when a nil io.Writer is passed to Put or Run
	ErrNilWriter = errors.New("nil writer provided")
)

// Sender is a method to handle messages.
type Sender func(*eridanus.Command) error

// Handler is a method to handle messages.
type Handler func(*eridanus.Command, Sender) error

// Get reads a message from the provided io.Reader.
func Get(r io.Reader) (*eridanus.Command, error) {
	if r == nil {
		return nil, ErrNilReader
	}

	slr := io.LimitReader(r, int64(msgSizeBytesLen))
	size := uint32(0)
	if err := binary.Read(slr, binary.LittleEndian, &size); err != nil {
		return nil, err
	}

	clr := io.LimitReader(r, int64(size))
	cmd := eridanus.Command{}
	if err := json.NewDecoder(clr).Decode(&cmd); err != nil {
		return nil, err
	}

	return &cmd, nil
}

// Put writes a message to the provided io.Writer.
func Put(w io.Writer, cmd *eridanus.Command) error {
	if w == nil {
		return ErrNilWriter
	}

	if cmd == nil {
		return ErrNilCommand
	}

	out, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(out))); err != nil {
		return err
	}
	if _, err := w.Write(out); err != nil {
		return err
	}
	return nil
}

// Run loops!
func Run(r io.Reader, w io.Writer, h Handler) error {
	for {
		cmd, err := Get(r)
		if err != nil {
			return err
		}
		if err := h(cmd, func(cmd *eridanus.Command) error {
			return Put(w, cmd)
		}); err != nil {
			return err
		}
	}
}
