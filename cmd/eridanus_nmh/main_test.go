package main

import (
	"io"
)

type failOnWrite struct{ count int }

func (f *failOnWrite) Write(b []byte) (int, error) {
	if f.count <= 0 {
		return 0, io.ErrShortWrite
	}
	f.count--
	return len(b), nil
}
