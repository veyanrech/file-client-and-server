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
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Addr  string `json:"addr"`
	Root  string `json:"root"`
	Token string `json:"token"`
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

	mux := http.NewServeMux()
	mux.HandleFunc("/manifest", auth(config, handleManifest(config)))
	mux.HandleFunc("/download/", auth(config, handleDownload(config)))

	log.Println("server started on", config.Addr)
	log.Println("root:", config.Root)

	log.Fatal(http.ListenAndServe(config.Addr, mux))
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

	if config.Addr == "" {
		return Config{}, fmt.Errorf("addr is required")
	}
	if config.Root == "" {
		return Config{}, fmt.Errorf("root is required")
	}
	if config.Token == "" {
		return Config{}, fmt.Errorf("token is required")
	}

	return config, nil
}

func auth(config Config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+config.Token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func handleManifest(config Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var files []FileInfo

		err := filepath.WalkDir(config.Root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				return nil
			}

			rel, err := filepath.Rel(config.Root, path)
			if err != nil {
				return err
			}

			rel = filepath.ToSlash(rel)

			info, err := d.Info()
			if err != nil {
				return err
			}

			hash, err := fileSHA256(path)
			if err != nil {
				return err
			}

			files = append(files, FileInfo{
				Path:    rel,
				Size:    info.Size(),
				ModTime: info.ModTime().Unix(),
				SHA256:  hash,
			})

			return nil
		})

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(files)
	}
}

func handleDownload(config Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/download/")
		name = filepath.Clean(name)

		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}

		fullPath := filepath.Join(config.Root, name)

		http.ServeFile(w, r, fullPath)
	}
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
