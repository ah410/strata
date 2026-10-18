package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
