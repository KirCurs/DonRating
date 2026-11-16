// Package config validates runtime settings before opening external resources.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Config struct {
	Addr         string
	DatabaseURL  string
	Devices      []Device
	RequestTTL   time.Duration
	AdminToken   string
	DeviceTokens map[string]string
	SlideshowURL string
}

var deviceID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func Load() (Config, error) { return load(os.Getenv) }

func load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:         valueOr(getenv("APP_ADDR"), "127.0.0.1:8080"),
		DatabaseURL:  valueOr(getenv("DATABASE_URL"), "host=/tmp port=5432 user=sh dbname=ratingdon"),
		RequestTTL:   30 * time.Second,
		AdminToken:   getenv("ADMIN_TOKEN"),
		DeviceTokens: make(map[string]string),
		SlideshowURL: getenv("SLIDESHOW_URL"),
	}
	host, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return Config{}, errors.New("APP_ADDR must contain a host and port")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return Config{}, errors.New("APP_ADDR must use a port from 1 to 65535")
	}
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	devicesJSON := valueOr(getenv("DEVICES_JSON"), `[{"id":"reception-1","name":"Ресепшен · планшет 1"}]`)
	if err := decode(devicesJSON, &cfg.Devices); err != nil || len(cfg.Devices) == 0 {
		return Config{}, errors.New("DEVICES_JSON must be a nonempty array of devices with id and name")
	}
	known := make(map[string]bool, len(cfg.Devices))
	for _, device := range cfg.Devices {
		if !deviceID.MatchString(device.ID) || strings.TrimSpace(device.Name) == "" {
			return Config{}, errors.New("DEVICES_JSON requires safe device ids (1–64 letters, digits, _ or -) and nonempty names")
		}
		if known[device.ID] {
			return Config{}, errors.New("DEVICES_JSON contains duplicate device ids")
		}
		known[device.ID] = true
	}
	if raw := getenv("REQUEST_TTL"); raw != "" {
		cfg.RequestTTL, err = time.ParseDuration(raw)
		if err != nil {
			return Config{}, errors.New("REQUEST_TTL must be a Go duration such as 30s or 2m")
		}
	}
	if cfg.RequestTTL < 5*time.Second || cfg.RequestTTL > 10*time.Minute {
		return Config{}, errors.New("REQUEST_TTL must be between 5s and 10m")
	}
	if raw := getenv("DEVICE_TOKENS_JSON"); raw != "" {
		if err := decode(raw, &cfg.DeviceTokens); err != nil || cfg.DeviceTokens == nil {
			return Config{}, errors.New("DEVICE_TOKENS_JSON must be an object of device ids and keys")
		}
	}
	if cfg.AdminToken != "" && !validToken(cfg.AdminToken) {
		return Config{}, errors.New("ADMIN_TOKEN must not contain whitespace or control characters")
	}
	if !loopback && len(cfg.AdminToken) < 24 {
		return Config{}, errors.New("ADMIN_TOKEN must contain at least 24 characters when APP_ADDR is not loopback")
	}
	usedTokens := make(map[string]bool)
	if cfg.AdminToken != "" {
		usedTokens[cfg.AdminToken] = true
	}
	for id, token := range cfg.DeviceTokens {
		if !known[id] {
			return Config{}, errors.New("DEVICE_TOKENS_JSON contains an unknown device id")
		}
		if token == "" || !validToken(token) {
			return Config{}, errors.New("DEVICE_TOKENS_JSON keys must be nonempty and contain no whitespace or control characters")
		}
		if usedTokens[token] {
			return Config{}, errors.New("device keys must be distinct from each other and from ADMIN_TOKEN")
		}
		usedTokens[token] = true
	}
	if !loopback {
		for _, device := range cfg.Devices {
			if len(cfg.DeviceTokens[device.ID]) < 24 {
				return Config{}, errors.New("each configured device requires a key of at least 24 characters when APP_ADDR is not loopback")
			}
		}
	}
	return cfg, nil
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func validToken(value string) bool {
	for _, char := range value {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func decode(raw string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("unexpected JSON after the first value")
	}
	return nil
}
