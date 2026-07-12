package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
)

type Config struct {
	Addr       string `json:"addr"`
	Root       string `json:"root"`
	Token      string `json:"token"`
	IgnoreFile string `json:"ignore_file"`
}

type IgnoreMatcher struct {
	rules []IgnoreRule
}

type IgnoreRule struct {
	Pattern       string
	Anchored      bool
	DirectoryOnly bool
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

	ignoreMatcher, err := loadIgnoreMatcher(config)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/manifest", auth(config, handleManifest(config, ignoreMatcher)))
	mux.HandleFunc("/download/", auth(config, handleDownload(config, ignoreMatcher)))

	log.Println("server started on", config.Addr)
	log.Println("root:", config.Root)
	log.Println("ignore file:", config.IgnoreFile)
	log.Println("ignore rules:", len(ignoreMatcher.rules))

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
	if config.IgnoreFile == "" {
		config.IgnoreFile = ".serverignore"
	}

	return config, nil
}

func loadIgnoreMatcher(config Config) (IgnoreMatcher, error) {
	ignorePath := config.IgnoreFile
	if !filepath.IsAbs(ignorePath) {
		ignorePath = filepath.Join(config.Root, ignorePath)
	}

	file, err := os.Open(ignorePath)
	if os.IsNotExist(err) {
		return IgnoreMatcher{}, nil
	}
	if err != nil {
		return IgnoreMatcher{}, err
	}
	defer file.Close()

	var rules []IgnoreRule

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		rule, ok := parseIgnoreRule(scanner.Text())
		if ok {
			rules = append(rules, rule)
		}
	}

	if err := scanner.Err(); err != nil {
		return IgnoreMatcher{}, err
	}

	return IgnoreMatcher{rules: rules}, nil
}

func parseIgnoreRule(line string) (IgnoreRule, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return IgnoreRule{}, false
	}

	line = filepath.ToSlash(line)

	anchored := strings.HasPrefix(line, "/")
	line = strings.TrimPrefix(line, "/")

	directoryOnly := strings.HasSuffix(line, "/")
	line = strings.TrimSuffix(line, "/")
	line = strings.TrimPrefix(line, "./")

	if line == "" {
		return IgnoreRule{}, false
	}

	return IgnoreRule{
		Pattern:       line,
		Anchored:      anchored,
		DirectoryOnly: directoryOnly,
	}, true
}

func (matcher IgnoreMatcher) Match(relPath string, isDir bool) bool {
	relPath = cleanRelPath(relPath)
	if relPath == "." {
		return false
	}

	for _, rule := range matcher.rules {
		if rule.Match(relPath, isDir) {
			return true
		}
	}

	return false
}

func (rule IgnoreRule) Match(relPath string, isDir bool) bool {
	parts := strings.Split(relPath, "/")

	if !strings.Contains(rule.Pattern, "/") {
		end := len(parts)
		if rule.Anchored {
			end = 1
		}

		for i, part := range parts[:end] {
			matched, err := pathpkg.Match(rule.Pattern, part)
			if err == nil && matched {
				partIsDir := i < len(parts)-1 || isDir
				if !rule.DirectoryOnly || partIsDir {
					return true
				}
			}
		}

		return false
	}

	start := 0
	end := 1
	if !rule.Anchored {
		end = len(parts)
	}

	for i := start; i < end; i++ {
		for j := i; j < len(parts); j++ {
			candidate := strings.Join(parts[i:j+1], "/")
			matched, err := pathpkg.Match(rule.Pattern, candidate)
			if err == nil && matched {
				candidateIsDir := j < len(parts)-1 || isDir
				if !rule.DirectoryOnly || candidateIsDir {
					return true
				}
			}
		}
	}

	return false
}

func cleanRelPath(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	path = strings.TrimPrefix(path, "./")
	return path
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

func handleManifest(config Config, ignoreMatcher IgnoreMatcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var files []FileInfo

		err := filepath.WalkDir(config.Root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			rel, err := filepath.Rel(config.Root, path)
			if err != nil {
				return err
			}

			rel = cleanRelPath(rel)

			if ignoreMatcher.Match(rel, d.IsDir()) {
				if d.IsDir() {
					return filepath.SkipDir
				}

				return nil
			}

			if d.IsDir() {
				return nil
			}

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

func handleDownload(config Config, ignoreMatcher IgnoreMatcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/download/")
		name = filepath.Clean(name)

		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}

		fullPath := filepath.Join(config.Root, name)

		info, err := os.Stat(fullPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		if ignoreMatcher.Match(name, info.IsDir()) {
			http.NotFound(w, r)
			return
		}

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
