package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func multipartUpload(t *testing.T, parts [][2]string) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		if p[0] == "relpath" {
			mw.WriteField("relpath", p[1])
			continue
		}
		fw, _ := mw.CreateFormFile("file", p[0])
		fw.Write([]byte(p[1]))
	}
	mw.Close()
	return buf.String(), mw.FormDataContentType()
}

func TestFolderUploadKeepsStructureSafely(t *testing.T) {
	h, dir := testServer(t)
	body, ct := multipartUpload(t, [][2]string{
		{"relpath", "Album/disc 1/01.flac"}, {"01.flac", "track"},
		{"plain.txt", "no relpath"},
	})
	rec := do(h, "POST", "/api/upload?path=music", "strata.example.com", body, map[string]string{"Content-Type": ct, "X-Strata": "1"})
	if rec.Code != 200 {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	for rel, want := range map[string]string{"music/Album/disc 1/01.flac": "track", "music/plain.txt": "no relpath"} {
		if b, err := os.ReadFile(filepath.Join(dir, rel)); err != nil || string(b) != want {
			t.Errorf("%s: %q %v", rel, b, err)
		}
	}

	for _, rel := range []string{"../escape.txt", "a/../../escape.txt", "a/.strata/x", "./x", "a//b"} {
		body, ct := multipartUpload(t, [][2]string{{"relpath", rel}, {"x", "evil"}})
		rec := do(h, "POST", "/api/upload?path=", "strata.example.com", body, map[string]string{"Content-Type": ct, "X-Strata": "1"})
		if rec.Code == 200 {
			t.Errorf("relpath %q accepted", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.txt")); err == nil {
		t.Fatal("upload escaped the root")
	}
}

func TestChunkedUploadOverHTTP(t *testing.T) {
	h, dir := testServer(t)
	post := map[string]string{"Content-Type": "application/json", "X-Strata": "1"}
	rec := do(h, "POST", "/api/uploads", "strata.example.com", `{"dir":"in","name":"big.bin","size":10}`, post)
	if rec.Code != 200 {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	var start struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &start)
	url := "/api/uploads/" + start.ID

	put := func(off, data string) *httptestRec {
		r := do(h, "PUT", url, "strata.example.com", data, map[string]string{"X-Strata": "1", "Upload-Offset": off, "Content-Type": "application/offset+octet-stream"})
		return &httptestRec{r.Code, r.Header().Get("Upload-Offset")}
	}
	if r := put("0", "hello"); r.code != http.StatusNoContent || r.off != "5" {
		t.Fatalf("first chunk: %+v", r)
	}
	if r := put("0", "hello"); r.code != http.StatusConflict || r.off != "5" {
		t.Fatalf("replayed chunk: %+v", r)
	}
	if r := do(h, "PUT", url, "strata.example.com", "world", map[string]string{"Upload-Offset": "5"}); r.Code != http.StatusForbidden {
		t.Fatalf("chunk without guard header: %d", r.Code)
	}
	if r := do(h, "HEAD", url, "strata.example.com", "", nil); r.Header().Get("Upload-Offset") != "5" {
		t.Fatalf("HEAD offset %q", r.Header().Get("Upload-Offset"))
	}
	if r := put("5", "world"); r.code != http.StatusNoContent {
		t.Fatalf("second chunk: %+v", r)
	}
	if r := do(h, "POST", url+"/finish", "strata.example.com", "", map[string]string{"X-Strata": "1"}); r.Code != 200 {
		t.Fatalf("finish: %d %s", r.Code, r.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "in/big.bin")); string(b) != "helloworld" {
		t.Fatalf("content %q", b)
	}
	if r := do(h, "HEAD", "/api/uploads/"+strings.Repeat("0", 24), "strata.example.com", "", nil); r.Code != http.StatusNotFound {
		t.Errorf("unknown upload: %d", r.Code)
	}
}

type httptestRec struct {
	code int
	off  string
}

func waitJob(t *testing.T, h http.Handler, id string) map[string]any {
	t.Helper()
	for range 400 {
		rec := do(h, "GET", "/api/jobs/"+id, "strata.example.com", "", nil)
		var j map[string]any
		json.Unmarshal(rec.Body.Bytes(), &j)
		if s := j["state"]; s != "queued" && s != "running" {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job never finished")
	return nil
}

func TestExtractOverHTTP(t *testing.T) {
	h, dir := testServer(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("inner/a.txt")
	f.Write([]byte("hello"))
	zw.Close()
	os.MkdirAll(filepath.Join(dir, "dl"), 0o755)
	os.WriteFile(filepath.Join(dir, "dl/takeout.zip"), buf.Bytes(), 0o644)

	hdr := map[string]string{"Content-Type": "application/json", "X-Strata": "1"}
	rec := do(h, "POST", "/api/extract", "strata.example.com", `{"path":"dl/takeout.zip"}`, hdr)
	if rec.Code != 200 {
		t.Fatalf("extract: %d %s", rec.Code, rec.Body)
	}
	var start struct{ Job string }
	json.Unmarshal(rec.Body.Bytes(), &start)
	j := waitJob(t, h, start.Job)
	if j["state"] != "done" || j["result"] != "dl/takeout" {
		t.Fatalf("job = %v", j)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "dl/takeout/inner/a.txt")); string(b) != "hello" {
		t.Fatalf("extracted = %q", b)
	}

	// Bad requests fail up front, not as a job.
	for _, body := range []string{`{"path":"dl/nope.zip"}`, `{"path":"dl/takeout"}`, `{"path":"../x.zip"}`} {
		if rec := do(h, "POST", "/api/extract", "strata.example.com", body, hdr); rec.Code == 200 {
			t.Errorf("%s accepted", body)
		}
	}
	if rec := do(h, "GET", "/api/jobs/nope", "strata.example.com", "", nil); rec.Code != 404 {
		t.Errorf("unknown job: %d", rec.Code)
	}
}

func TestPreviewOverHTTP(t *testing.T) {
	h, dir := testServer(t)
	os.WriteFile(filepath.Join(dir, "n.md"), []byte("# hi\nthere"), 0o644)
	os.WriteFile(filepath.Join(dir, "p.bin"), []byte("x"), 0o644)
	rec := do(h, "GET", "/api/preview?path=n.md&m=1", "strata.example.com", "", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"lines":["# hi","there"]`) {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/preview?path=p.bin", "strata.example.com", "", nil); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("unsupported: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/preview?path=.strata/x.md", "strata.example.com", "", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("internal: %d", rec.Code)
	}
}

func TestCompressOverHTTP(t *testing.T) {
	h, dir := testServer(t)
	os.MkdirAll(filepath.Join(dir, "a"), 0o755)
	os.WriteFile(filepath.Join(dir, "a/f.txt"), []byte("f"), 0o644)
	hdr := map[string]string{"Content-Type": "application/json", "X-Strata": "1"}
	rec := do(h, "POST", "/api/compress", "strata.example.com", `{"paths":["a"],"dir":""}`, hdr)
	if rec.Code != 200 {
		t.Fatalf("compress: %d %s", rec.Code, rec.Body)
	}
	var start struct{ Job string }
	json.Unmarshal(rec.Body.Bytes(), &start)
	if j := waitJob(t, h, start.Job); j["state"] != "done" || j["result"] != "a.zip" {
		t.Fatalf("job = %v", j)
	}
	for _, body := range []string{`{"paths":[],"dir":""}`, `{"paths":["a"],"dir":"a/f.txt"}`, `{"paths":["a"],"dir":".strata"}`, `{"paths":["../a"],"dir":"nope"}`} {
		if rec := do(h, "POST", "/api/compress", "strata.example.com", body, hdr); rec.Code == 200 {
			t.Errorf("%s accepted", body)
		}
	}
}
