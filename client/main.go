package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	ServerURL       string `json:"server_url"`
	Root            string `json:"root"`
	Token           string `json:"token"`
	IntervalSeconds int    `json:"interval_seconds"`
}

type FileInfo struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
	SHA256  string `json:"sha256"`
}

func main() {
	configPath := flag.String("config", "config.json", "path to JSON config file")
	flag.Parse()

	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	if err := os.MkdirAll(config.Root, 0755); err != nil {
		log.Fatal(err)
	}

	for {
		if err := syncOnce(config); err != nil {
			log.Println("sync error:", err)
		}

		time.Sleep(time.Duration(config.IntervalSeconds) * time.Second)
	}
}

func loadConfig(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	var config Config

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&config); err != nil {
		return Config{}, err
	}

	config.ServerURL = strings.TrimRight(config.ServerURL, "/")

	if config.ServerURL == "" {
		return Config{}, fmt.Errorf("server_url is required")
	}
	if config.Root == "" {
		return Config{}, fmt.Errorf("root is required")
	}
	if config.Token == "" {
		return Config{}, fmt.Errorf("token is required")
	}
	if config.IntervalSeconds <= 0 {
		return Config{}, fmt.Errorf("interval_seconds must be greater than 0")
	}

	return config, nil
}

func syncOnce(config Config) error {
	files, err := getManifest(config)
	if err != nil {
		return err
	}

	for _, f := range files {
		localPath := filepath.Join(config.Root, filepath.FromSlash(f.Path))

		needDownload := true

		if _, err := os.Stat(localPath); err == nil {
			hash, err := fileSHA256(localPath)
			if err == nil && hash == f.SHA256 {
				needDownload = false
			}
		}

		if needDownload {
			log.Println("downloading:", f.Path)

			if err := downloadFile(config, f.Path, localPath); err != nil {
				log.Println("download failed:", f.Path, err)
				continue
			}
		}
	}

	return nil
}

func getManifest(config Config) ([]FileInfo, error) {
	req, err := http.NewRequest("GET", config.ServerURL+"/manifest", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+config.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest request failed: %s", resp.Status)
	}

	var files []FileInfo

	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, err
	}

	return files, nil
}

func downloadFile(config Config, remotePath string, localPath string) error {
	escaped := url.PathEscape(remotePath)

	req, err := http.NewRequest("GET", config.ServerURL+"/download/"+escaped, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+config.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download request failed for %q: %s", remotePath, resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}

	tmp := localPath + ".tmp"

	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return err
	}

	if err := out.Close(); err != nil {
		return err
	}

	return os.Rename(tmp, localPath)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()

	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
