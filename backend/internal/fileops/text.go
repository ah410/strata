package fileops

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"path"
	"strings"

	"strata/internal/safepath"
	"strata/internal/userr"
)

// TextMax is the largest text file shown or edited in the browser.
const TextMax = 2 << 20

// ErrConflict means the file changed on disk since the editor loaded it.
var ErrConflict = userr.Error("this file changed since you opened it; reload to see the new version")

// Text is a text file's content plus a version for safe saving.
type Text struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	Version   string `json:"version"`
}

func textVersion(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12])
}

// ReadText returns up to TextMax bytes of rel.
func ReadText(root *safepath.Root, rel string) (*Text, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return nil, err
	}
	f, err := root.FS().Open(rel)
	if err != nil {
		return nil, safepath.Err(err)
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return nil, userr.Error("not a file")
	}
	buf, err := io.ReadAll(io.LimitReader(f, TextMax+1))
	if err != nil {
		return nil, err
	}
	t := &Text{Version: textVersion(buf)}
	if len(buf) > TextMax {
		buf, t.Truncated, t.Version = buf[:TextMax], true, ""
	}
	t.Content = string(buf)
	return t, nil
}

// WriteText saves content to rel. version must match the file as last read;
// an empty version creates a new file and refuses to replace one.
func WriteText(root *safepath.Root, rel, version, content string) (string, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return "", err
	}
	if len(content) > TextMax {
		return "", userr.Error("text is too large to save here (limit 2 MB)")
	}
	if strings.HasPrefix(path.Base(rel), ".strata") {
		return "", userr.Error("invalid file name")
	}
	cur, err := ReadText(root, rel)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if version != "" {
			return "", ErrConflict // deleted meanwhile
		}
	case err != nil:
		return "", err
	case version == "" || cur.Truncated || cur.Version != version:
		return "", ErrConflict
	}
	if err := WriteAtomic(root, rel, func(w io.Writer) error {
		_, err := io.WriteString(w, content)
		return err
	}); err != nil {
		return "", err
	}
	return textVersion([]byte(content)), nil
}
