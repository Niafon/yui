// Package modelsettings persists owner-added model endpoints and choices
// without rewriting the installation's primary configuration file.
package modelsettings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/yui-companion/core/internal/inference"
	"github.com/yui-companion/core/internal/model"
)

type Data struct {
	Providers   []model.ProviderConfig `json:"providers"`
	Defaults    map[string]string      `json:"defaults"`
	Preferences *inference.Preferences `json:"preferences,omitempty"`
}

type Store struct {
	mu   sync.Mutex
	path string
	data Data
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, data: Data{Defaults: map[string]string{}}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, err
	}
	if s.data.Defaults == nil {
		s.data.Defaults = map[string]string{}
	}
	return s, nil
}

func (s *Store) Snapshot() Data {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.data)
}

func (s *Store) AddProvider(c model.ProviderConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.data)
	for _, existing := range next.Providers {
		if existing.ID == c.ID {
			return errors.New("modelsettings: duplicate provider id")
		}
	}
	next.Providers = append(next.Providers, c)
	return s.commit(next)
}

func (s *Store) RemoveProvider(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.data)
	for i, c := range next.Providers {
		if c.ID == id {
			next.Providers = append(next.Providers[:i], next.Providers[i+1:]...)
			return s.commit(next)
		}
	}
	return nil
}

func (s *Store) SetDefault(kind, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.data)
	next.Defaults[kind] = id
	return s.commit(next)
}

func (s *Store) SetPreferences(p inference.Preferences) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.data)
	next.Preferences = &p
	return s.commit(next)
}

func clone(in Data) Data {
	out := Data{
		Providers: append([]model.ProviderConfig(nil), in.Providers...),
		Defaults:  make(map[string]string, len(in.Defaults)),
	}
	for k, v := range in.Defaults {
		out.Defaults[k] = v
	}
	if in.Preferences != nil {
		p := *in.Preferences
		out.Preferences = &p
	}
	return out
}

func (s *Store) commit(next Data) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".model-settings-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return err
	}
	s.data = next
	return nil
}
