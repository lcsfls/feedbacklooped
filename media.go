package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

var errUnsupported = errors.New("file type not allowed (allowed: PNG, JPG, GIF, WebP, MP4, MOV, WebM, TXT/LOG)")

// sniff detects the type from the content, not the file extension.
func sniff(head []byte, name string) (mime, ext string, err error) {
	switch {
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", ".png", nil
	case bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg", ".jpg", nil
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		return "image/gif", ".gif", nil
	case len(head) >= 12 && string(head[0:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return "image/webp", ".webp", nil
	case bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}):
		return "video/webm", ".webm", nil
	case len(head) >= 12 && string(head[4:8]) == "ftyp":
		if string(head[8:12]) == "qt  " {
			return "video/quicktime", ".mov", nil
		}
		return "video/mp4", ".mp4", nil
	}
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".log") {
		if utf8.Valid(trimIncompleteRune(head)) && !bytes.ContainsRune(head, 0) {
			return "text/plain; charset=utf-8", ".txt", nil
		}
	}
	return "", "", errUnsupported
}

func trimIncompleteRune(b []byte) []byte {
	for i := 0; i < 4 && len(b) > 0; i++ {
		if utf8.Valid(b) {
			return b
		}
		b = b[:len(b)-1]
	}
	return b
}

// saveUpload validates, cleans and stores an uploaded file.
func (a *App) saveUpload(fh *multipart.FileHeader, maxBytes int64) (Attachment, error) {
	if fh.Size > maxBytes {
		return Attachment{}, fmt.Errorf("%s is larger than %d MB", fh.Filename, maxBytes>>20)
	}
	f, err := fh.Open()
	if err != nil {
		return Attachment{}, err
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	mime, ext, err := sniff(head, fh.Filename)
	if err != nil {
		return Attachment{}, fmt.Errorf("%s: %w", fh.Filename, err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Attachment{}, err
	}

	id := randomToken(12)
	file := id + ext
	dst := filepath.Join(a.store.Uploads, file)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o640)
	if err != nil {
		return Attachment{}, err
	}
	var size int64
	if strings.HasPrefix(mime, "image/") && mime != "image/gif" {
		data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
		if err == nil {
			data = stripMetadata(mime, data)
			var w int
			w, err = out.Write(data)
			size = int64(w)
		}
		if err != nil {
			out.Close()
			os.Remove(dst)
			return Attachment{}, err
		}
	} else {
		size, err = io.Copy(out, io.LimitReader(f, maxBytes+1))
		if err != nil {
			out.Close()
			os.Remove(dst)
			return Attachment{}, err
		}
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return Attachment{}, err
	}
	if size > maxBytes {
		os.Remove(dst)
		return Attachment{}, fmt.Errorf("%s is larger than %d MB", fh.Filename, maxBytes>>20)
	}
	return Attachment{
		ID: id, Name: cleanFilename(fh.Filename, ext), Mime: mime, Size: size,
		File: file, CreatedAt: time.Now(),
	}, nil
}

func cleanFilename(name, ext string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	if len(name) > 120 {
		name = name[:120]
	}
	if name == "" || name == "." || name == "blob" {
		name = "file" + ext
	}
	return name
}

// stripMetadata removes EXIF/XMP/comments without re-encoding the image.
// On parse errors the file is left unchanged.
func stripMetadata(mime string, b []byte) []byte {
	var out []byte
	var ok bool
	switch mime {
	case "image/jpeg":
		out, ok = stripJPEG(b)
	case "image/png":
		out, ok = stripPNG(b)
	case "image/webp":
		out, ok = stripWebP(b)
	}
	if !ok {
		return b
	}
	return out
}

func stripJPEG(b []byte) ([]byte, bool) {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return nil, false
	}
	out := make([]byte, 0, len(b))
	out = append(out, 0xFF, 0xD8)
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return nil, false
		}
		marker := b[i+1]
		if marker == 0xFF { // fill byte
			i++
			continue
		}
		if marker == 0xDA { // Start of Scan: copy the rest verbatim
			out = append(out, b[i:]...)
			return out, true
		}
		if marker == 0xD9 {
			out = append(out, b[i:i+2]...)
			return out, true
		}
		if marker >= 0xD0 && marker <= 0xD7 {
			out = append(out, b[i:i+2]...)
			i += 2
			continue
		}
		segLen := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		end := i + 2 + segLen
		if segLen < 2 || end > len(b) {
			return nil, false
		}
		// Drop APP1 (EXIF/XMP), APP13 (IPTC), COM (comment).
		if marker != 0xE1 && marker != 0xED && marker != 0xFE {
			out = append(out, b[i:end]...)
		}
		i = end
	}
	return nil, false
}

func stripPNG(b []byte) ([]byte, bool) {
	if len(b) < 8 {
		return nil, false
	}
	out := make([]byte, 0, len(b))
	out = append(out, b[:8]...)
	i := 8
	for i+12 <= len(b) {
		n := int(binary.BigEndian.Uint32(b[i : i+4]))
		typ := string(b[i+4 : i+8])
		end := i + 12 + n
		if n < 0 || end > len(b) {
			return nil, false
		}
		switch typ {
		case "eXIf", "tEXt", "zTXt", "iTXt", "tIME":
		default:
			out = append(out, b[i:end]...)
		}
		i = end
		if typ == "IEND" {
			return out, true
		}
	}
	return nil, false
}

func stripWebP(b []byte) ([]byte, bool) {
	if len(b) < 12 {
		return nil, false
	}
	body := make([]byte, 0, len(b))
	vp8xAt := -1
	i := 12
	for i+8 <= len(b) {
		typ := string(b[i : i+4])
		n := int(binary.LittleEndian.Uint32(b[i+4 : i+8]))
		end := i + 8 + n + n%2
		if end > len(b) {
			if i+8+n == len(b) { // tolerate missing padding at end of file
				end = len(b)
			} else {
				return nil, false
			}
		}
		if typ != "EXIF" && typ != "XMP " {
			if typ == "VP8X" {
				vp8xAt = len(body)
			}
			body = append(body, b[i:end]...)
		}
		i = end
	}
	if vp8xAt >= 0 && vp8xAt+8 < len(body) {
		body[vp8xAt+8] &^= 0x08 | 0x04 // clear EXIF and XMP flags
	}
	out := make([]byte, 12, 12+len(body))
	copy(out, b[:12])
	binary.LittleEndian.PutUint32(out[4:8], uint32(4+len(body)))
	return append(out, body...), true
}
