package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/runloom/workflows/internal/prompts"
	"github.com/example/runloom/workflows/internal/recordset"
)

var ErrPromptConflict = errors.New("prompt changed; refresh before saving")
var ErrInvalidPrompt = errors.New("invalid prompt")

type PromptState struct {
	Drafts   map[string]prompts.Entry `json:"drafts"`
	Versions map[string]prompts.Entry `json:"versions"`
	History  map[string]prompts.Entry `json:"history"`
}
type promptStore struct{ repo recordset.Repository }

func (s *promptStore) read(ctx context.Context) (PromptState, int64, error) {
	c := PromptState{map[string]prompts.Entry{}, map[string]prompts.Entry{}, map[string]prompts.Entry{}}
	r, rev, err := s.repo.Read(ctx)
	if err == nil {
		err = recordset.Decode(r, &c)
	}
	return c, rev, err
}
func (s *promptStore) ListPrompts(ctx context.Context) ([]prompts.Entry, error) {
	c, _, err := s.read(ctx)
	result := []prompts.Entry{}
	for _, e := range c.Drafts {
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, err
}
func (s *promptStore) GetPrompt(ctx context.Context, id string, version int) (prompts.Entry, error) {
	c, _, err := s.read(ctx)
	if err != nil {
		return prompts.Entry{}, err
	}
	var e prompts.Entry
	var ok bool
	if version == 0 {
		e, ok = c.Drafts[id]
	} else {
		e, ok = c.Versions[fmt.Sprintf("%s/%d", id, version)]
	}
	if !ok {
		return e, ErrNotFound
	}
	return e, nil
}
func (s *promptStore) PromptHistory(ctx context.Context, id string) ([]prompts.Entry, error) {
	c, _, err := s.read(ctx)
	result := []prompts.Entry{}
	for _, e := range c.History {
		if e.ID == id {
			result = append(result, e)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Revision > result[j].Revision })
	return result, err
}
func (s *promptStore) SavePrompt(ctx context.Context, e prompts.Entry, publish bool, actor string) (prompts.Entry, error) {
	tags, err := prompts.NormalizeTags(e.Tags)
	if err != nil {
		return e, fmt.Errorf("%w: %v", ErrInvalidPrompt, err)
	}
	e.Tags = tags
	if e.Template.Variables == nil {
		e.Template.Variables = []prompts.Variable{}
	}
	if !prompts.Name.MatchString(e.ID) || strings.TrimSpace(e.Name) == "" || len(e.Name) > 120 || len(e.Description) > 2000 {
		return e, fmt.Errorf("%w: ID, name or description", ErrInvalidPrompt)
	}
	if err := prompts.Validate(e.Template); err != nil {
		return e, fmt.Errorf("%w: %v", ErrInvalidPrompt, err)
	}
	for attempt := 0; attempt < 16; attempt++ {
		c, rev, err := s.read(ctx)
		if err != nil {
			return e, err
		}
		previous, exists := c.Drafts[e.ID]
		if e.Revision != previous.Revision {
			return e, ErrPromptConflict
		}
		if (!exists && len(c.Drafts) >= 200) || len(c.History) >= 10000 {
			return e, fmt.Errorf("%w: library capacity reached", ErrInvalidPrompt)
		}
		next := e
		next.Revision++
		next.Actor = actor
		next.UpdatedAt = utcNow()
		next.Version = previous.Version
		next.Published = publish
		next.Hash = prompts.Hash(e.Template)
		if publish {
			next.Version++
			c.Versions[fmt.Sprintf("%s/%d", e.ID, next.Version)] = next
		}
		c.Drafts[e.ID] = next
		c.History[fmt.Sprintf("%s/%d", e.ID, next.Revision)] = next
		r, err := recordset.Encode(c)
		if err != nil {
			return e, err
		}
		ok, err := s.repo.Swap(ctx, rev, r)
		if err != nil {
			return e, err
		}
		if ok {
			return next, nil
		}
	}
	return e, ErrPromptConflict
}

// File mode is single-process like the workflow catalog; database adapters use CAS transactions.
type promptFile struct{ store *Store }
type promptFileDocument struct {
	Revision int64             `json:"revision"`
	Records  recordset.Records `json:"records"`
}

func (p promptFile) read() (promptFileDocument, error) {
	var d promptFileDocument
	err := p.store.readJSON(filepath.Join(p.store.root, "_prompts", "library.json"), &d)
	if errors.Is(err, ErrNotFound) {
		err = nil
	}
	if d.Records == nil {
		d.Records = recordset.Records{}
	}
	return d, err
}
func (p promptFile) Read(ctx context.Context) (recordset.Records, int64, error) {
	p.store.mu.RLock()
	defer p.store.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	d, err := p.read()
	return d.Records, d.Revision, err
}
func (p promptFile) Swap(ctx context.Context, rev int64, r recordset.Records) (bool, error) {
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	d, err := p.read()
	if err != nil || rev != d.Revision {
		return false, err
	}
	err = p.store.writeJSON(filepath.Join(p.store.root, "_prompts", "library.json"), promptFileDocument{rev + 1, r})
	return err == nil, err
}
