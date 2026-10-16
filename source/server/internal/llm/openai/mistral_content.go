package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const mistralContentLimit = 8 << 20

// normalizeMistralContent translates documented text/thinking chunks into the
// SDK's text/reasoning fields. It never guesses how to replay signed reasoning.
func normalizeMistralContent(data []byte) ([]byte, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, errors.New("invalid Mistral completion JSON")
	}
	var choices []map[string]json.RawMessage
	if err := json.Unmarshal(envelope["choices"], &choices); err != nil {
		return data, nil
	}
	changed := false
	for _, choice := range choices {
		for _, field := range []string{"message", "delta"} {
			var message map[string]json.RawMessage
			if json.Unmarshal(choice[field], &message) != nil {
				continue
			}
			content := bytes.TrimSpace(message["content"])
			if len(content) == 0 || content[0] != '[' {
				continue
			}
			var chunks []struct {
				Type      string `json:"type"`
				Text      string `json:"text"`
				Signature string `json:"signature"`
				Thinking  []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"thinking"`
			}
			if json.Unmarshal(content, &chunks) != nil {
				return nil, errors.New("invalid Mistral content chunks")
			}
			var text, reasoning strings.Builder
			for _, chunk := range chunks {
				switch chunk.Type {
				case "text":
					text.WriteString(chunk.Text)
				case "thinking":
					if chunk.Signature != "" {
						return nil, errors.New("Mistral signed reasoning is not supported; choose a model without signed reasoning")
					}
					for _, thought := range chunk.Thinking {
						if thought.Type != "text" {
							return nil, errors.New("unsupported Mistral thinking chunk")
						}
						reasoning.WriteString(thought.Text)
					}
				default:
					return nil, errors.New("unsupported Mistral response content chunk")
				}
			}
			message["content"], _ = json.Marshal(text.String())
			if reasoning.Len() > 0 {
				message["reasoning_content"], _ = json.Marshal(reasoning.String())
			}
			choice[field], _ = json.Marshal(message)
			changed = true
		}
	}
	if !changed {
		return data, nil
	}
	envelope["choices"], _ = json.Marshal(choices)
	return json.Marshal(envelope)
}

// A lazy, bounded SSE transformer: no goroutine and no whole-stream buffering.
type mistralContentReader struct {
	source  io.ReadCloser
	scanner *bufio.Scanner
	pending []byte
	err     error
}

func (r *mistralContentReader) Close() error { return r.source.Close() }
func (r *mistralContentReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(r.pending) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		if !r.scanner.Scan() {
			r.err = r.scanner.Err()
			if r.err == nil {
				r.err = io.EOF
			}
			return 0, r.err
		}
		line := r.scanner.Bytes()
		if bytes.HasPrefix(line, []byte("data:")) {
			payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if len(payload) > 0 && !bytes.Equal(payload, []byte("[DONE]")) {
				converted, err := normalizeMistralContent(payload)
				if err != nil {
					r.err = err
					return 0, err
				}
				line = append([]byte("data: "), converted...)
			}
		}
		r.pending = append(append([]byte(nil), line...), '\n')
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func normalizeMistralResponse(resp *http.Response) (*http.Response, error) {
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4096), mistralContentLimit)
		resp.Body = &mistralContentReader{source: resp.Body, scanner: scanner}
		resp.ContentLength = -1
		resp.Header.Del("Content-Length")
		return resp, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, mistralContentLimit+1))
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if len(data) > mistralContentLimit {
		return nil, errors.New("Mistral completion exceeds response limit")
	}
	data, err = normalizeMistralContent(data)
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(data))
	resp.ContentLength = int64(len(data))
	resp.Header.Del("Content-Length")
	return resp, nil
}
