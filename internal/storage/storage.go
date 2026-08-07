package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	indexFilename = ".lan-drop-index.json"
	uploadPrefix  = ".lan-drop-upload-"
	indexPrefix   = ".lan-drop-index-"
)

var (
	ErrInvalidName = errors.New("invalid filename")
	ErrNotFound    = errors.New("file not found")
	ErrTooLarge    = errors.New("file exceeds the size limit")
)

type File struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	SHA256   string    `json:"sha256"`
}

type Store struct {
	root      string
	indexPath string

	mu    sync.RWMutex
	files map[string]File
}

type diskIndex struct {
	Files []File `json:"files"`
}

func New(root string) (*Store, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o750); err != nil {
		return nil, fmt.Errorf("create root: %w", err)
	}

	s := &Store{
		root:      absRoot,
		indexPath: filepath.Join(absRoot, indexFilename),
		files:     make(map[string]File),
	}
	if err := s.loadAndScan(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Root() string {
	return s.root
}

func (s *Store) List() []File {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files := make([]File, 0, len(s.files))
	for _, file := range s.files {
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Modified.Equal(files[j].Modified) {
			return files[i].Name < files[j].Name
		}
		return files[i].Modified.After(files[j].Modified)
	})
	return files
}

func (s *Store) Save(ctx context.Context, filename string, source io.Reader, maxBytes int64) (File, error) {
	name, err := normalizeUploadName(filename)
	if err != nil {
		return File{}, err
	}

	temp, err := os.CreateTemp(s.root, uploadPrefix)
	if err != nil {
		return File{}, fmt.Errorf("create temporary file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)

	hash := sha256.New()
	limited := io.LimitReader(&contextReader{ctx: ctx, reader: source}, maxBytes+1)
	written, copyErr := io.Copy(io.MultiWriter(temp, hash), limited)
	closeErr := temp.Close()
	if copyErr != nil {
		return File{}, fmt.Errorf("write upload: %w", copyErr)
	}
	if closeErr != nil {
		return File{}, fmt.Errorf("close upload: %w", closeErr)
	}
	if written > maxBytes {
		return File{}, ErrTooLarge
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	name = s.uniqueNameLocked(name)
	destination := filepath.Join(s.root, name)
	if err := os.Rename(tempName, destination); err != nil {
		return File{}, fmt.Errorf("publish upload: %w", err)
	}
	if err := os.Chmod(destination, 0o640); err != nil {
		_ = os.Remove(destination)
		return File{}, fmt.Errorf("set file permissions: %w", err)
	}

	info, err := os.Stat(destination)
	if err != nil {
		return File{}, fmt.Errorf("inspect upload: %w", err)
	}
	file := File{
		Name:     name,
		Size:     info.Size(),
		Modified: info.ModTime().UTC(),
		SHA256:   hex.EncodeToString(hash.Sum(nil)),
	}
	s.files[name] = file
	if err := s.writeIndexLocked(); err != nil {
		delete(s.files, name)
		_ = os.Remove(destination)
		return File{}, err
	}
	return file, nil
}

func (s *Store) Open(name string) (*os.File, File, error) {
	if err := validateStoredName(name); err != nil {
		return nil, File{}, err
	}

	s.mu.RLock()
	meta, exists := s.files[name]
	s.mu.RUnlock()
	if !exists {
		return nil, File{}, ErrNotFound
	}

	file, err := os.Open(filepath.Join(s.root, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, File{}, ErrNotFound
	}
	if err != nil {
		return nil, File{}, fmt.Errorf("open file: %w", err)
	}
	return file, meta, nil
}

func (s *Store) Delete(name string) error {
	if err := validateStoredName(name); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.files[name]; !exists {
		return ErrNotFound
	}
	if err := os.Remove(filepath.Join(s.root, name)); errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("delete file: %w", err)
	}
	delete(s.files, name)
	if err := s.writeIndexLocked(); err != nil {
		return err
	}
	return nil
}

func (s *Store) loadAndScan() error {
	previous := make(map[string]File)
	data, err := os.ReadFile(s.indexPath)
	if err == nil {
		var index diskIndex
		if json.Unmarshal(data, &index) == nil {
			for _, file := range index.Files {
				previous[file.Name] = file
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read metadata index: %w", err)
	}

	entries, err := os.ReadDir(s.root)
	if err != nil {
		return fmt.Errorf("scan shared directory: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == indexFilename || strings.HasPrefix(name, uploadPrefix) || strings.HasPrefix(name, indexPrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}

		meta := previous[name]
		modified := info.ModTime().UTC()
		if meta.Size != info.Size() || !meta.Modified.Equal(modified) || meta.SHA256 == "" {
			digest, err := hashFile(filepath.Join(s.root, name))
			if err != nil {
				return fmt.Errorf("hash %q: %w", name, err)
			}
			meta = File{Name: name, Size: info.Size(), Modified: modified, SHA256: digest}
		}
		s.files[name] = meta
	}

	if err := s.writeIndexLocked(); err != nil {
		return err
	}
	return nil
}

func (s *Store) uniqueNameLocked(name string) string {
	if _, exists := s.files[name]; !exists {
		if _, err := os.Stat(filepath.Join(s.root, name)); errors.Is(err, os.ErrNotExist) {
			return name
		}
	}

	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, exists := s.files[candidate]; exists {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.root, candidate)); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
}

func (s *Store) writeIndexLocked() error {
	files := make([]File, 0, len(s.files))
	for _, file := range s.files {
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	temp, err := os.CreateTemp(s.root, indexPrefix)
	if err != nil {
		return fmt.Errorf("create metadata index: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)

	encoder := json.NewEncoder(temp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(diskIndex{Files: files}); err != nil {
		temp.Close()
		return fmt.Errorf("encode metadata index: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync metadata index: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close metadata index: %w", err)
	}
	if err := os.Rename(tempName, s.indexPath); err != nil {
		return fmt.Errorf("publish metadata index: %w", err)
	}
	return nil
}

func normalizeUploadName(name string) (string, error) {
	name = strings.TrimSpace(name)
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || name == "." || name == ".." || name == indexFilename {
		return "", ErrInvalidName
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", ErrInvalidName
		}
	}
	return name, nil
}

func validateStoredName(name string) error {
	normalized, err := normalizeUploadName(name)
	if err != nil || normalized != name || strings.ContainsAny(name, "/\\") {
		return ErrInvalidName
	}
	return nil
}

func hashFile(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(p)
	}
}
