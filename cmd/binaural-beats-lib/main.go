// Package main builds as a C shared library (.so/.dylib/.dll) for FFI use.
// It exposes the binaural beats engine via a single ProcessRPC function
// that accepts JSON-RPC 2.0 requests and returns JSON-RPC 2.0 responses.
//
// Build with: go build -buildmode=c-shared -o libbinaural.so ./cmd/binaural-beats-lib/
package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"unsafe"

	"github.com/Wundark/binaural-beats/internal/engine"
	"github.com/Wundark/binaural-beats/internal/rpc"
)

var eng *engine.Engine

func init() {
	eng = engine.NewEngine()
}

// BinauralProcessRPC takes a JSON-RPC 2.0 request string and returns a JSON-RPC 2.0 response string.
// The caller must free the returned string with BinauralFreeString.
//
//export BinauralProcessRPC
func BinauralProcessRPC(input *C.char) *C.char {
	requestJSON := C.GoString(input)
	responseJSON := rpc.ProcessRequest(eng, requestJSON)
	return C.CString(responseJSON)
}

// BinauralFreeString frees a string previously returned by BinauralProcessRPC.
//
//export BinauralFreeString
func BinauralFreeString(s *C.char) {
	C.free(unsafe.Pointer(s))
}

func main() {}
