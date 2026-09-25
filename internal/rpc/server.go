package rpc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/Wundark/binaural-beats/internal/engine"
	"github.com/Wundark/binaural-beats/internal/presets"
)

// JSON-RPC 2.0 request/response types.

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type LoadConfigParams struct {
	Path string `json:"path"`
}

type LoadPresetParams struct {
	ID string `json:"id"`
}

type ExportWAVParams struct {
	Path string `json:"path"`
}

type SetStretchParams struct {
	Factor float64 `json:"factor"`
}

type SeekParams struct {
	Time float64 `json:"time"`
}

type SetVolumeParams struct {
	Volume float64 `json:"volume"`
}

// ProcessRequest handles a single JSON-RPC request string and returns a JSON response string.
// This is the core handler used by both the stdin/stdout server and FFI bindings.
func ProcessRequest(eng *engine.Engine, requestJSON string) string {
	var req Request
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		resp := Response{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: -32700, Message: "Parse error"},
			ID:      nil,
		}
		data, _ := json.Marshal(resp)
		return string(data)
	}

	resp := handleRequest(eng, req)
	data, _ := json.Marshal(resp)
	return string(data)
}

// Server handles JSON-RPC requests over stdin/stdout.
type Server struct {
	eng    *engine.Engine
	reader *bufio.Reader
	writer io.Writer
}

func NewServer(eng *engine.Engine) *Server {
	return &Server{
		eng:    eng,
		reader: bufio.NewReader(os.Stdin),
		writer: os.Stdout,
	}
}

func (s *Server) Run() error {
	for {
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read error: %w", err)
		}

		resp := ProcessRequest(s.eng, string(line))
		s.writer.Write([]byte(resp))
		s.writer.Write([]byte("\n"))
	}
}

func handleRequest(eng *engine.Engine, req Request) Response {
	resp := Response{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "load_config":
		var params LoadConfigParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
			return resp
		}
		info, err := eng.LoadConfig(params.Path)
		if err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = info

	case "get_timeline":
		timeline, err := eng.Timeline()
		if err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = timeline

	case "list_presets":
		list, err := presets.List()
		if err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = list

	case "load_preset":
		var params LoadPresetParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
			return resp
		}
		info, err := presets.Load(eng, params.ID)
		if err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = info

	case "play":
		if err := eng.Play(); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = map[string]interface{}{"ok": true}

	case "stop":
		if err := eng.Stop(); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = map[string]interface{}{"ok": true}

	case "get_status":
		status := eng.GetStatus()
		resp.Result = status

	case "export_wav":
		var params ExportWAVParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
			return resp
		}
		if err := eng.ExportWAV(params.Path); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = map[string]interface{}{"ok": true}

	case "set_stretch":
		var params SetStretchParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
			return resp
		}
		if err := eng.SetStretch(params.Factor); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = map[string]interface{}{"ok": true}

	case "pause":
		if err := eng.Pause(); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = map[string]interface{}{"ok": true}

	case "resume":
		if err := eng.Resume(); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = map[string]interface{}{"ok": true}

	case "seek":
		var params SeekParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
			return resp
		}
		if err := eng.Seek(params.Time); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = map[string]interface{}{"ok": true}

	case "set_volume":
		var params SetVolumeParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
			return resp
		}
		if err := eng.SetVolume(params.Volume); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = map[string]interface{}{"ok": true}

	default:
		resp.Error = &RPCError{Code: -32601, Message: "Method not found: " + req.Method}
	}

	return resp
}
