package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Settings struct {
	Address        string `json:"address"`
	User           string `json:"user"`
	Password       string `json:"password"`
	Port           int    `json:"port"`
	HTTPProxyPort  int    `json:"http_proxy_port"`
	SocksProxyPort int    `json:"socks_proxy_port"`
	ResilienceMode bool   `json:"resilience_mode"` // true = Network Resilience Mode
	FastReconnect  bool   `json:"fast_reconnect"`  // true = Fast Reconnect Mode
	BlackList      string `json:"blacklist"`       // comma/newline separated; these do NOT go through SSH
	Autostart      bool   `json:"autostart"`       // launch app + start proxy on system startup
}

// --- password encryption (hardcoded key, minimum security, as requested) ---

var encKey = []byte("cipherproxy-static-aes256-keys!!") // exactly 32 bytes => AES-256

const encPrefix = "enc:"

func encryptPassword(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ct), nil
}

func decryptPassword(s string) (string, error) {
	if !strings.HasPrefix(s, encPrefix) {
		return s, nil // legacy plaintext
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, encPrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(b) < ns {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := b[:ns], b[ns:]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func Dir() (string, error) {
	base, err := os.UserConfigDir() // resolves to ~/.config on Linux
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "cipher-proxy"), nil
}

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load returns settings with the password DECRYPTED (ready to use). Any legacy
// plaintext password on disk is migrated to encrypted form automatically.
func Load() (*Settings, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Settings{}, nil // no config yet — return zero-value defaults
		}
		return nil, err
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}

	if s.Password != "" && !strings.HasPrefix(s.Password, encPrefix) {
		// legacy plaintext on disk -> migrate to encrypted
		if enc, e := encryptPassword(s.Password); e == nil && enc != "" {
			_ = writeEncrypted(s, enc)
		}
		return &s, nil // returned as plaintext for use
	}
	if s.Password != "" {
		if dec, e := decryptPassword(s.Password); e == nil {
			s.Password = dec
		}
	}
	return &s, nil
}

// Save writes settings with the password ENCRYPTED (idempotent).
func Save(s *Settings) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	pw := s.Password
	if pw != "" && !strings.HasPrefix(pw, encPrefix) {
		if enc, e := encryptPassword(pw); e == nil && enc != "" {
			pw = enc
		}
	}
	tmp := *s
	tmp.Password = pw
	data, err := json.MarshalIndent(&tmp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func writeEncrypted(s Settings, enc string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	tmp := s
	tmp.Password = enc
	data, err := json.MarshalIndent(&tmp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// BlackListEntries parses the BlackList field into a clean slice.
func (s *Settings) BlackListEntries() []string {
	out := make([]string, 0)
	for _, part := range strings.FieldsFunc(s.BlackList, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	}) {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}
