// Embedded image scenes follow Quiet.md board/encode.rs (MIT): Excalidraw
// bstring JSON wraps zlib bytes. PNG and SVG preserve normal image rendering.
package whiteboard

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
	"unicode/utf8"
)

const SceneMIME = "application/vnd.excalidraw+json"

var ErrNoScene = errors.New("whiteboard: image has no editable scene")

type encodedData struct {
	Version    string `json:"version"`
	Encoding   string `json:"encoding"`
	Compressed bool   `json:"compressed"`
	Encoded    string `json:"encoded"`
}

func byteString(b []byte) string {
	r := make([]rune, len(b))
	for i, v := range b {
		r[i] = rune(v)
	}
	return string(r)
}
func byteStringBytes(s string) ([]byte, error) {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 255 {
			return nil, fmt.Errorf("whiteboard: invalid byte string")
		}
		b = append(b, byte(r))
	}
	return b, nil
}
func encodePayload(text string) ([]byte, error) {
	if len(text) > MaxSceneBytes {
		return nil, fmt.Errorf("whiteboard: scene exceeds size limit")
	}
	var compressed bytes.Buffer
	z := zlib.NewWriter(&compressed)
	if _, err := z.Write([]byte(text)); err != nil {
		return nil, err
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(encodedData{Version: "1", Encoding: "bstring", Compressed: true, Encoded: byteString(compressed.Bytes())})
	if err != nil {
		return nil, err
	}
	return byteStringBytes(string(b))
}
func decodePayload(raw []byte) (string, error) {
	// Early ArcMD exports embed UTF-8 scene JSON without the bstring wrapper.
	var legacy struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &legacy) == nil && legacy.Type == "excalidraw" {
		return string(raw), nil
	}
	var d encodedData
	if err := json.Unmarshal([]byte(byteString(raw)), &d); err != nil {
		return "", fmt.Errorf("whiteboard: invalid image payload: %w", err)
	}
	if d.Encoding != "bstring" || (d.Version != "" && d.Version != "1") {
		return "", fmt.Errorf("whiteboard: unsupported image payload")
	}
	b, err := byteStringBytes(d.Encoded)
	if err != nil {
		return "", err
	}
	if d.Compressed {
		z, err := zlib.NewReader(bytes.NewReader(b))
		if err != nil {
			return "", err
		}
		defer z.Close()
		b, err = io.ReadAll(io.LimitReader(z, MaxSceneBytes+1))
		if err != nil {
			return "", err
		}
	}
	if len(b) > MaxSceneBytes || !utf8.Valid(b) {
		return "", fmt.Errorf("whiteboard: invalid or oversized scene")
	}
	return string(b), nil
}

func EncodeSVGPayload(text string) (string, error) {
	b, err := encodePayload(text)
	if err != nil {
		return "", err
	}
	return "<!-- payload-type:" + SceneMIME + " --><!-- payload-version:2 --><!-- payload-start -->" + base64.StdEncoding.EncodeToString(b) + "<!-- payload-end -->", nil
}

func DecodeSVGPayload(svg string) (string, error) {
	if !strings.Contains(svg, "payload-type:"+SceneMIME) {
		return "", ErrNoScene
	}
	if _, rest, ok := strings.Cut(svg, "<!-- payload-version:"); ok {
		version, _, closed := strings.Cut(rest, " -->")
		if !closed || (version != "1" && version != "2") {
			return "", fmt.Errorf("whiteboard: unsupported SVG payload version")
		}
	}
	_, rest, ok := strings.Cut(svg, "<!-- payload-start -->")
	if !ok {
		return "", ErrNoScene
	}
	text, _, ok := strings.Cut(rest, "<!-- payload-end -->")
	if !ok {
		return "", fmt.Errorf("whiteboard: incomplete SVG payload")
	}
	if len(text) > MaxSceneBytes*2 {
		return "", fmt.Errorf("whiteboard: SVG payload exceeds size limit")
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil {
		return "", err
	}
	return decodePayload(b)
}

var pngSignature = []byte{137, 80, 78, 71, 13, 10, 26, 10}

type pngChunk struct {
	name string
	data []byte
}

func readPNGChunks(data []byte) ([]pngChunk, error) {
	if len(data) > MaxSceneBytes || !bytes.HasPrefix(data, pngSignature) {
		return nil, fmt.Errorf("whiteboard: invalid or oversized PNG")
	}
	var out []pngChunk
	for i := 8; i+12 <= len(data); {
		n := int64(binary.BigEndian.Uint32(data[i : i+4]))
		end64 := int64(i) + 8 + n
		if end64+4 > int64(len(data)) {
			return nil, fmt.Errorf("whiteboard: truncated PNG")
		}
		end := int(end64)
		if crc32.ChecksumIEEE(data[i+4:end]) != binary.BigEndian.Uint32(data[end:end+4]) {
			return nil, fmt.Errorf("whiteboard: PNG checksum mismatch")
		}
		name := string(data[i+4 : i+8])
		out = append(out, pngChunk{name, data[i+8 : end]})
		i = end + 4
		if name == "IEND" {
			return out, nil
		}
	}
	return nil, fmt.Errorf("whiteboard: incomplete PNG")
}
func writePNGChunks(chunks []pngChunk) []byte {
	out := append([]byte{}, pngSignature...)
	for _, c := range chunks {
		out = binary.BigEndian.AppendUint32(out, uint32(len(c.data)))
		start := len(out)
		out = append(out, c.name...)
		out = append(out, c.data...)
		out = binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(out[start:]))
	}
	return out
}
func EncodePNGMetadata(png []byte, text string) ([]byte, error) {
	chunks, err := readPNGChunks(png)
	if err != nil {
		return nil, err
	}
	payload, err := encodePayload(text)
	if err != nil {
		return nil, err
	}
	var out []pngChunk
	for _, c := range chunks {
		if c.name == "tEXt" && bytes.HasPrefix(c.data, []byte(SceneMIME+"\x00")) {
			continue
		}
		if c.name == "IEND" {
			out = append(out, pngChunk{"tEXt", append([]byte(SceneMIME+"\x00"), payload...)})
		}
		out = append(out, c)
	}
	return writePNGChunks(out), nil
}
func DecodePNGMetadata(png []byte) (string, error) {
	chunks, err := readPNGChunks(png)
	if err != nil {
		return "", err
	}
	for _, c := range chunks {
		if c.name == "tEXt" && bytes.HasPrefix(c.data, []byte(SceneMIME+"\x00")) {
			return decodePayload(c.data[len(SceneMIME)+1:])
		}
	}
	return "", ErrNoScene
}
