package erasurecode

/*
#cgo pkg-config: erasurecode-1
#include <liberasurecode/erasurecode.h>
*/
import "C"

import (
	"fmt"
	"io"
	"os"
)

type ECWriter struct {
	Backend *ErasureCodeBackend
	Writers []io.WriteCloser
}

type ECReader struct {
	Backend *ErasureCodeBackend
	Readers []io.ReadCloser
	buffer  []byte
}

func getWriters(prefix string, n uint8, perm os.FileMode) ([]io.WriteCloser, error) {
	var i, j uint8
	writers := make([]io.WriteCloser, n)
	for i = 0; i < n; i++ {
		fname := fmt.Sprintf("%s#%d", prefix, i)
		file, err := os.OpenFile(fname, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
		if err != nil {
			// Clean up the writers we *did* open
			for j = 0; i < j; j++ {
				// Ignoring any errors allong the way
				writers[i].Close()
			}
			return nil, err
		}
		writers[i] = file
	}
	return writers, nil
}

func getReaders(prefix string, n uint8) ([]io.ReadCloser, error) {
	var i, j uint8
	readers := make([]io.ReadCloser, n)
	for i = 0; i < n; i++ {
		file, err := os.Open(fmt.Sprintf("%s#%d", prefix, i))
		if err != nil {
			// Clean up the readers we *did* open
			for j = 0; i < j; j++ {
				// Ignoring any errors allong the way
				readers[i].Close()
			}
			return nil, err
		}
		readers[i] = file
	}
	return readers, nil
}

func (shim ECWriter) Write(p []byte) (int, error) {
	frags, err := shim.Backend.Encode(p)
	if err != nil {
		return 0, err
	}
	for i, writer := range shim.Writers {
		// TODO: check for errors
		writer.Write(frags[i])
	}
	return len(p), nil
}

func (shim ECWriter) Close() error {
	var firstErr error
	for _, writer := range shim.Writers {
		err := writer.Close()
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (shim ECReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("Not implemented")
	n := copy(p, shim.buffer)
	shim.buffer = shim.buffer[n:]
	p = p[n:]
	if len(p) == 0 {
		return n, nil
	}

	// TODO: This shit needs a lot of work

	frags := make([][]byte, len(shim.Readers))
	// Read one fragment header from each stream
	// Check that they all agree on how big the fragments should be
	// Read the rest of the fragment from each stream
	data, err := shim.Backend.Decode(frags)
	shim.buffer = data
	if err != nil {
		return 0, err
	}
	for i, reader := range shim.Readers {
		// TODO: check for errors
		reader.Read(frags[i])
	}
	return len(p), nil
}

func (shim ECReader) Close() error {
	var firstErr error
	for _, reader := range shim.Readers {
		err := reader.Close()
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	shim.buffer = nil
	return firstErr
}

func (backend *ErasureCodeBackend) GetFileWriter(prefix string, perm os.FileMode) (io.WriteCloser, error) {
	writers, err := getWriters(prefix, uint8(backend.K+backend.M), perm)
	if err != nil {
		return nil, err
	}
	return ECWriter{backend, writers}, nil
}

func ReadFragment(reader io.Reader) ([]byte, error) {
	header := make([]byte, C.sizeof_struct_fragment_header_s)
	n, err := io.ReadFull(reader, header)
	if err != nil {
		return header[:n], err
	}
	info := GetFragmentInfo(header)

	if !info.IsValid {
		return header, fmt.Errorf("Metadata checksum failed")
	}

	frag := make([]byte, len(header)+info.Size)
	copy(frag, header)
	n, err = io.ReadFull(reader, frag[n:])
	if err != nil {
		return frag[:len(header)+n], err
	}

	return frag, nil
}
