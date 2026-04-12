package nm

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

type handshakeRequest struct {
	Type        string `json:"type"`
	ExtensionID string `json:"extensionId"`
}

type handshakeResponse struct {
	Type  string `json:"type"`
	Port  int    `json:"port,omitempty"`
	Token string `json:"token,omitempty"`
	Error string `json:"error,omitempty"`
}

// Session holds the port and auth token written by the server.
type Session struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

// DataDir returns the platform-specific directory where vbm stores its data.
//   - Linux/macOS: ~/.local/share/vbm
//   - Windows:     %APPDATA%\vbm
func DataDir() (string, error) {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("APPDATA environment variable not set")
		}
		return filepath.Join(appData, "vbm"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "vbm"), nil
}

// SessionPath returns the path to the session file.
// Returns an error if the data directory cannot be determined.
func SessionPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

func readSession() (*Session, error) {
	path, err := SessionPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Session
	return &s, json.Unmarshal(data, &s)
}

// WriteSession writes the session file (chmod 600).
func WriteSession(s *Session) error {
	path, err := SessionPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// RunHost is the NM host entrypoint. Reads one message from stdin, responds, exits.
func RunHost() error {
	msg, err := readMessage(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading NM message: %w", err)
	}

	var req handshakeRequest
	if err := json.Unmarshal(msg, &req); err != nil {
		return writeMessage(os.Stdout, handshakeResponse{Type: "handshake_error", Error: "invalid json"})
	}

	if req.Type != "handshake" {
		return writeMessage(os.Stdout, handshakeResponse{Type: "handshake_error", Error: "expected handshake"})
	}

	session, err := readSession()
	if err != nil {
		return writeMessage(os.Stdout, handshakeResponse{Type: "handshake_error", Error: fmt.Sprintf("daemon not running: %v", err)})
	}

	return writeMessage(os.Stdout, handshakeResponse{
		Type:  "handshake_ok",
		Port:  session.Port,
		Token: session.Token,
	})
}

func readMessage(r io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	if length > 1024*1024 {
		return nil, fmt.Errorf("message too large: %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeMessage(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	length := uint32(len(data))
	if err := binary.Write(w, binary.LittleEndian, length); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
