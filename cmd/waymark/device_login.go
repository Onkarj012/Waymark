package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type discoveryResponse struct {
	ControlURL          string `json:"control_url"`
	ContentURL          string `json:"content_url"`
	DeviceAuthorization bool   `json:"device_authorization"`
}

type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

func cmdLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	serverFlag := fs.String("server", "", "content or control server URL (prompted if omitted)")
	deviceName := fs.String("device-name", "", "label shown to the owner during approval")
	readOnly := fs.Bool("read-only", false, "request only pages:read")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("login does not accept positional arguments")
	}
	return loginWithDevice(*serverFlag, *deviceName, *readOnly)
}

func loginWithDevice(serverValue, deviceName string, readOnly bool) error {
	existing, err := loadConfig()
	if err != nil {
		return err
	}
	server := firstNonEmpty(serverValue, os.Getenv("WAYMARK_URL"))
	if server == "" {
		hint := ""
		if existing.URL != "" {
			hint = " [" + existing.URL + "]"
		}
		value, err := readLine("Server URL" + hint + ": ")
		if err != nil {
			return err
		}
		server = firstNonEmpty(value, existing.URL)
	}
	server, err = normalizeServerURL(server)
	if err != nil {
		return err
	}

	discovery, err := discover(server)
	if err != nil {
		return fmt.Errorf("device discovery failed: %w; upgrade the Waymark deployment, then retry", err)
	}
	if !discovery.DeviceAuthorization || discovery.ControlURL == "" {
		return errors.New("this instance does not support device login; upgrade the Waymark deployment, then retry")
	}
	controlURL, err := normalizeServerURL(discovery.ControlURL)
	if err != nil {
		return fmt.Errorf("server returned an unsafe control URL: %w", err)
	}
	if deviceName == "" {
		host, hostErr := os.Hostname()
		if hostErr != nil || strings.TrimSpace(host) == "" {
			host = "this device"
		}
		deviceName = "waymark on " + host
	}
	if len(deviceName) > 120 {
		return errors.New("--device-name must be 120 characters or fewer")
	}
	secret, err := randomDeviceSecret()
	if err != nil {
		return err
	}
	scopes := []string{"pages:read", "pages:write"}
	if readOnly {
		scopes = []string{"pages:read"}
	}
	code, err := requestDeviceCode(controlURL, secret, deviceName, scopes)
	if err != nil {
		return err
	}
	fmt.Printf("Open this URL to approve the device:\n%s\n\nCode: %s\n\nWaiting for approval...\n", code.VerificationURIComplete, code.UserCode)
	token, err := pollDeviceToken(controlURL, code, secret)
	if err != nil {
		return err
	}
	if err := saveConfig(config{URL: controlURL, Token: token}); err != nil {
		return err
	}
	fmt.Printf("✓ Logged in to %s\n", controlURL)
	fmt.Printf("  saved to %s\n", configPath())
	return nil
}

func discover(server string) (discoveryResponse, error) {
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Get(server + "/.well-known/waymark")
	if err != nil {
		return discoveryResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return discoveryResponse{}, fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}
	var discovery discoveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return discovery, err
	}
	return discovery, nil
}

func requestDeviceCode(controlURL, secret, label string, scopes []string) (deviceCodeResponse, error) {
	payload, err := json.Marshal(map[string]any{"device_secret": secret, "device_label": label, "scopes": scopes})
	if err != nil {
		return deviceCodeResponse{}, err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Post(controlURL+"/api/auth/device/code", "application/json", bytes.NewReader(payload))
	if err != nil {
		return deviceCodeResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return deviceCodeResponse{}, responseError(resp)
	}
	var code deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&code); err != nil {
		return code, err
	}
	if code.DeviceCode == "" || code.UserCode == "" || code.VerificationURI == "" || code.ExpiresIn <= 0 {
		return code, errors.New("server returned an incomplete device authorization response")
	}
	if code.Interval < 1 {
		code.Interval = 5
	}
	return code, nil
}

func pollDeviceToken(controlURL string, code deviceCodeResponse, secret string) (string, error) {
	deadline := time.Now().Add(time.Duration(code.ExpiresIn) * time.Second)
	interval := time.Duration(code.Interval) * time.Second
	for time.Now().Before(deadline) {
		payload, err := json.Marshal(map[string]string{"device_code": code.DeviceCode, "device_secret": secret})
		if err != nil {
			return "", err
		}
		resp, err := (&http.Client{Timeout: 15 * time.Second}).Post(controlURL+"/api/auth/device/token", "application/json", bytes.NewReader(payload))
		if err != nil {
			return "", err
		}
		var result struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if decodeErr != nil {
			return "", decodeErr
		}
		if resp.StatusCode == http.StatusOK && result.AccessToken != "" {
			return result.AccessToken, nil
		}
		switch result.Error {
		case "authorization_pending":
		case "slow_down":
			if retry, err := time.ParseDuration(resp.Header.Get("Retry-After") + "s"); err == nil && retry > interval {
				interval = retry
			} else {
				interval += 5 * time.Second
			}
		case "access_denied":
			return "", errors.New("device authorization was denied")
		case "expired_token":
			return "", errors.New("device authorization expired")
		default:
			return "", fmt.Errorf("device authorization failed: %s", result.Error)
		}
		time.Sleep(interval)
	}
	return "", errors.New("device authorization expired")
}

func randomDeviceSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func responseError(resp *http.Response) error {
	data, _ := io.ReadAll(resp.Body)
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &body) == nil && body.Error != "" {
		return fmt.Errorf("server %d: %s", resp.StatusCode, body.Error)
	}
	return fmt.Errorf("server %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
}
