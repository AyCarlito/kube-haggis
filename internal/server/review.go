package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// mutationReviewHeader carries the opaque review handle between browser and server.
	mutationReviewHeader = "X-KubeHaggis-Review"
	// mutationReviewTTL bounds how long a successful dry-run can authorize a final mutation.
	mutationReviewTTL = 2 * time.Minute
	// maxMutationReviews bounds the number of live review entries retained by one server.
	maxMutationReviews = 1024
)

var errMutationReviewRejected = errors.New("mutation review is missing, expired, or does not match this request")

// reviewBinding contains only safe coordinates, hashes, and confirmation scope for one mutation review.
type reviewBinding struct {
	Context                 string
	Group                   string
	Version                 string
	Resource                string
	Namespace               string
	Name                    string
	Namespaced              bool
	ExpectedResourceVersion string
	Action                  string
	ParametersHash          [sha256.Size]byte
	BodyHash                [sha256.Size]byte
	HasBodyHash             bool
	ItemsHash               [sha256.Size]byte
	HasItemsHash            bool
	ConfirmationScope       string
}

// matches reports whether two mutation requests have exactly the same review binding.
func (binding reviewBinding) matches(candidate reviewBinding) bool {
	if binding.Context != candidate.Context || binding.Group != candidate.Group || binding.Version != candidate.Version || binding.Resource != candidate.Resource || binding.Namespace != candidate.Namespace || binding.Name != candidate.Name || binding.Namespaced != candidate.Namespaced || binding.ExpectedResourceVersion != candidate.ExpectedResourceVersion || binding.Action != candidate.Action || binding.HasBodyHash != candidate.HasBodyHash || binding.HasItemsHash != candidate.HasItemsHash || binding.ConfirmationScope != candidate.ConfirmationScope {
		return false
	}
	if subtle.ConstantTimeCompare(binding.ParametersHash[:], candidate.ParametersHash[:]) != 1 {
		return false
	}
	if binding.HasBodyHash && subtle.ConstantTimeCompare(binding.BodyHash[:], candidate.BodyHash[:]) != 1 {
		return false
	}
	return !binding.HasItemsHash || subtle.ConstantTimeCompare(binding.ItemsHash[:], candidate.ItemsHash[:]) == 1
}

// mutationReviewEntry stores one expiring, one-use server-bound review.
type mutationReviewEntry struct {
	binding   reviewBinding
	issuedAt  time.Time
	expiresAt time.Time
}

// mutationReviewStore is a bounded concurrent store for opaque mutation review handles.
type mutationReviewStore struct {
	mu         sync.Mutex
	entries    map[string]mutationReviewEntry
	now        func() time.Time
	ttl        time.Duration
	maxEntries int
}

// newMutationReviewStore creates a production review store with bounded expiry and capacity.
func newMutationReviewStore() *mutationReviewStore {
	return &mutationReviewStore{
		entries:    make(map[string]mutationReviewEntry),
		now:        time.Now,
		ttl:        mutationReviewTTL,
		maxEntries: maxMutationReviews,
	}
}

// currentTime returns the injected review clock or the wall clock when no test clock is configured.
func (store *mutationReviewStore) currentTime() time.Time {
	if store.now != nil {
		return store.now()
	}
	return time.Now()
}

// issue creates one cryptographically random opaque handle for a review binding.
func (store *mutationReviewStore) issue(binding reviewBinding) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.entries == nil {
		store.entries = make(map[string]mutationReviewEntry)
	}
	now := store.currentTime()
	store.pruneExpiredLocked(now)
	maxEntries := store.maxEntries
	if maxEntries < 1 {
		maxEntries = maxMutationReviews
	}
	for len(store.entries) >= maxEntries {
		store.removeOldestLocked()
	}
	ttl := store.ttl
	if ttl <= 0 {
		ttl = mutationReviewTTL
	}
	for attempt := 0; attempt < 4; attempt++ {
		token, err := randomMutationReviewToken()
		if err != nil {
			return "", errors.New("could not create mutation review")
		}
		if _, exists := store.entries[token]; exists {
			continue
		}
		store.entries[token] = mutationReviewEntry{binding: binding, issuedAt: now, expiresAt: now.Add(ttl)}
		return token, nil
	}
	return "", errors.New("could not create mutation review")
}

// consume validates and atomically removes one matching review before Kubernetes mutation.
func (store *mutationReviewStore) consume(token string, binding reviewBinding) error {
	if strings.TrimSpace(token) == "" {
		return errMutationReviewRejected
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.entries == nil {
		return errMutationReviewRejected
	}
	now := store.currentTime()
	store.pruneExpiredLocked(now)
	entry, found := store.entries[token]
	if !found || !entry.expiresAt.After(now) || !entry.binding.matches(binding) {
		return errMutationReviewRejected
	}
	delete(store.entries, token)
	return nil
}

// clear removes every outstanding review, such as after an explicit kubeconfig reload.
func (store *mutationReviewStore) clear() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.entries = make(map[string]mutationReviewEntry)
}

// pruneExpiredLocked removes reviews that are no longer usable; callers must hold store.mu.
func (store *mutationReviewStore) pruneExpiredLocked(now time.Time) {
	for token, entry := range store.entries {
		if !entry.expiresAt.After(now) {
			delete(store.entries, token)
		}
	}
}

// removeOldestLocked evicts the oldest live entry to preserve the store capacity bound.
func (store *mutationReviewStore) removeOldestLocked() {
	var oldestToken string
	var oldestTime time.Time
	for token, entry := range store.entries {
		if oldestToken == "" || entry.issuedAt.Before(oldestTime) {
			oldestToken = token
			oldestTime = entry.issuedAt
		}
	}
	if oldestToken != "" {
		delete(store.entries, oldestToken)
	}
}

// randomMutationReviewToken returns a URL-safe token with no encoded request material.
func randomMutationReviewToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// hashReviewParameters hashes normalized structured action parameters without retaining their values.
func hashReviewParameters(value interface{}) [sha256.Size]byte {
	encoded, _ := json.Marshal(value)
	return sha256.Sum256(encoded)
}

// mutationReviews returns the lazily initialized per-server review store.
func (s *Server) mutationReviews() *mutationReviewStore {
	s.reviewStoreMu.Lock()
	defer s.reviewStoreMu.Unlock()
	if s.reviewStore == nil {
		s.reviewStore = newMutationReviewStore()
	}
	return s.reviewStore
}

// writeMutationReviewHeader issues a safe opaque handle after a successful server-side dry-run.
func (s *Server) writeMutationReviewHeader(writer http.ResponseWriter, binding reviewBinding) bool {
	token, err := s.mutationReviews().issue(binding)
	if err != nil {
		writeError(writer, errors.New("server-side mutation review is unavailable"))
		return false
	}
	writer.Header().Set(mutationReviewHeader, token)
	return true
}

// consumeMutationReview validates and consumes the handle presented for a final mutation.
func (s *Server) consumeMutationReview(request *http.Request, binding reviewBinding) error {
	return s.mutationReviews().consume(request.Header.Get(mutationReviewHeader), binding)
}

// invalidateMutationReviews removes all handles after a kubeconfig reload boundary.
func (s *Server) invalidateMutationReviews() {
	s.mutationReviews().clear()
}

// mutationReviewContext resolves an omitted context before it is bound to a review.
func (s *Server) mutationReviewContext(request *http.Request) string {
	contextName := strings.TrimSpace(request.URL.Query().Get("context"))
	if contextName != "" || s.kube == nil {
		return contextName
	}
	snapshot, _ := s.kube.Contexts()
	return strings.TrimSpace(snapshot.CurrentContext)
}
