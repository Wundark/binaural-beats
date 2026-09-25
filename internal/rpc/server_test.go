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
	if st.Time != 30 || st.Frequency != 150 || st.Volume != 0.5 || st.Stretch != 1 || st.TotalDuration != 60 {
		t.Fatalf("status %+v", st)
	}
}

func TestPresets(t *testing.T) {
	eng := engine.NewEngine()
	r := call(t, eng, `{"jsonrpc":"2.0","method":"list_presets","id":1}`)
	var list []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if r.Error != nil || json.Unmarshal(r.Result, &list) != nil || len(list) == 0 {
		t.Fatalf("list_presets: %+v %s", r.Error, r.Result)
	}
	r = call(t, eng, `{"jsonrpc":"2.0","method":"load_preset","params":{"id":"`+list[0].ID+`"},"id":2}`)
	var info engine.SessionInfo
	if r.Error != nil || json.Unmarshal(r.Result, &info) != nil || info.Name != list[0].Name || info.TotalDuration <= 0 {
		t.Fatalf("load_preset: %+v %s", r.Error, r.Result)
	}
	if r := call(t, eng, `{"jsonrpc":"2.0","method":"load_preset","params":{"id":"nope"},"id":3}`); r.Error == nil {
		t.Fatal("unknown preset should fail")
	}
}

func TestTimelineIsStretched(t *testing.T) {
	eng := engine.NewEngine()
	if r := call(t, eng, `{"jsonrpc":"2.0","method":"get_timeline","id":1}`); r.Error == nil {
		t.Fatal("get_timeline without a session should fail")
	}
	loadTestConfig(t, eng)
	call(t, eng, `{"jsonrpc":"2.0","method":"set_stretch","params":{"factor":2},"id":2}`)
	r := call(t, eng, `{"jsonrpc":"2.0","method":"get_timeline","id":3}`)
	var tl engine.Timeline
	if r.Error != nil || json.Unmarshal(r.Result, &tl) != nil {
		t.Fatalf("get_timeline: %+v %s", r.Error, r.Result)
	}
	if tl.TotalDuration != 120 || tl.Name != "c" || len(tl.Changes) != 2 || tl.Changes[1].Time != 120 || tl.Changes[1].Frequency != 200 {
		t.Fatalf("timeline %+v", tl)
	}
}
