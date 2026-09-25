package rpc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wundark/binaural-beats/internal/engine"
)

type testResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
	ID     interface{}     `json:"id"`
}

func call(t *testing.T, eng *engine.Engine, req string) testResponse {
	t.Helper()
	var resp testResponse
	if err := json.Unmarshal([]byte(ProcessRequest(eng, req)), &resp); err != nil {
		t.Fatalf("bad response JSON for %s: %v", req, err)
	}
	return resp
}

func loadTestConfig(t *testing.T, eng *engine.Engine) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c.yaml")
	cfg := "frequency_changes:\n  - time: 0\n    frequency: 100\n    tone_volume: 1\n  - time: 60\n    frequency: 200\n    tone_volume: 1\n"
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	pathJSON, _ := json.Marshal(path)
	if r := call(t, eng, `{"jsonrpc":"2.0","method":"load_config","params":{"path":`+string(pathJSON)+`},"id":1}`); r.Error != nil {
		t.Fatalf("load_config: %s", r.Error.Message)
	}
}

func TestErrors(t *testing.T) {
	eng := engine.NewEngine()
	cases := map[string]int{
		`not json`: -32700,
		`{"jsonrpc":"2.0","method":"nope","id":1}`:                                  -32601,
		`{"jsonrpc":"2.0","method":"seek","params":{"time":"x"},"id":1}`:            -32602,
		`{"jsonrpc":"2.0","method":"set_volume","params":{"volume":2},"id":1}`:      -32000,
		`{"jsonrpc":"2.0","method":"pause","id":1}`:                                 -32000,
		`{"jsonrpc":"2.0","method":"load_config","params":{"path":"/nope"},"id":1}`: -32000,
	}
	for req, code := range cases {
		r := call(t, eng, req)
		if r.Error == nil || r.Error.Code != code {
			t.Errorf("%s: got %+v, want error code %d", req, r.Error, code)
		}
	}
}

func TestSeekAndVolumeShowInStatus(t *testing.T) {
	eng := engine.NewEngine()
	loadTestConfig(t, eng)
	for _, req := range []string{
		`{"jsonrpc":"2.0","method":"seek","params":{"time":30},"id":2}`,
		`{"jsonrpc":"2.0","method":"set_volume","params":{"volume":0.5},"id":3}`,
	} {
		if r := call(t, eng, req); r.Error != nil {
			t.Fatalf("%s: %s", req, r.Error.Message)
		}
	}
	r := call(t, eng, `{"jsonrpc":"2.0","method":"get_status","id":"s"}`)
	if r.ID != "s" {
		t.Fatalf("response id %v, want \"s\"", r.ID)
	}
	var st engine.Status
	if err := json.Unmarshal(r.Result, &st); err != nil {
		t.Fatal(err)
	}
	if st.Time != 30 || st.Frequency != 150 || st.Volume != 0.5 || st.TotalDuration != 60 {
		t.Fatalf("status %+v", st)
	}
}
