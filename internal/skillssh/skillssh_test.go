package skillssh

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/legibet/myskills/internal/source"
)

// newServer serves handler and returns a client for it.
func newServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL + "/", HTTP: srv.Client()}
}

func TestNew(t *testing.T) {
	c := New()
	if c.BaseURL != DefaultBaseURL || c.HTTP == nil || c.HTTP.Timeout != 15*time.Second {
		t.Errorf("New() = %+v", c)
	}
}

func TestSearch(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			http.NotFound(w, r)
			return
		}
		if q := r.URL.Query(); q.Get("q") != "swift ui&more" || q.Get("limit") != "3" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		if ua := r.UserAgent(); ua != "myskills" {
			t.Errorf("User-Agent = %q", ua)
		}
		_, _ = w.Write([]byte(`{"query":"swift ui&more","searchType":"fuzzy","skills":[
			{"id":"b/r/z","source":"b/r","skillId":"z","name":"Zed","installs":5,"extra":true},
			{"id":"a/r/y","source":"a/r","skillId":"y","name":"y","installs":12853},
			{"id":"a/r/x","source":"a/r","skillId":"x","name":"x"},
			{"source":"https://evil.example/r","skillId":"a"},
			{"source":"a/r/sub","skillId":"a"},
			{"source":"a/r","skillId":""}
		],"count":6}`))
	})

	got, err := c.Search(t.Context(), "swift ui&more", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{
		{Source: "b/r", SkillID: "z", Name: "Zed", Installs: 5},
		{Source: "a/r", SkillID: "y", Name: "y", Installs: 12853},
		{Source: "a/r", SkillID: "x", Name: "x"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Search = %+v\nwant %+v", got, want)
	}
}

func TestResultSpec(t *testing.T) {
	want, err := source.Parse("a/r@x")
	if err != nil {
		t.Fatal(err)
	}
	if got := (Result{Source: "a/r", SkillID: "x"}).Spec(); !reflect.DeepEqual(got, want) {
		t.Errorf("Spec = %+v\nwant %+v", got, want)
	}
}

func TestSearchErrors(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("q") {
		case "down":
			http.Error(w, "busy", http.StatusServiceUnavailable)
		case "garbage":
			_, _ = w.Write([]byte("<html>"))
		case "slow":
			<-r.Context().Done()
		}
	})

	if _, err := c.Search(t.Context(), "down", 10); err == nil || err.Error() != "skills.sh search: 503 Service Unavailable" {
		t.Errorf("error = %v", err)
	}
	if _, err := c.Search(t.Context(), "garbage", 10); err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Errorf("error = %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Search(ctx, "slow", 10); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want context.DeadlineExceeded", err)
	}
}

func TestSkillMarkdown(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if ua := r.UserAgent(); ua != "myskills" {
			t.Errorf("User-Agent = %q", ua)
		}
		switch r.URL.EscapedPath() {
		case "/api/download/o/r/exact":
			_, _ = w.Write([]byte(`{"files":[
				{"path":"sub/SKILL.md","contents":"nested"},
				{"path":"SKILL.md","contents":"---\nname: exact\n---\n"},
				{"path":"README.md","contents":"readme"}
			],"hash":"abc"}`))
		case "/api/download/o/r/nested":
			_, _ = w.Write([]byte(`{"files":[
				{"path":"NOTSKILL.md","contents":"no"},
				{"path":"skills/nested/SKILL.md","contents":"nested"}
			]}`))
		case "/api/download/o/r/none":
			_, _ = w.Write([]byte(`{"files":[{"path":"README.md","contents":"readme"}]}`))
		case "/api/download/my%20org/r%3F/a%2Fb":
			_, _ = w.Write([]byte(`{"files":[{"path":"SKILL.md","contents":"escaped"}]}`))
		case "/api/download/o/r/down":
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not_found"}`))
		}
	})

	tests := []struct {
		source, skill, want string
	}{
		{"o/r", "exact", "---\nname: exact\n---\n"},
		{"o/r", "nested", "nested"},
		{"my org/r?", "a/b", "escaped"},
	}
	for _, tt := range tests {
		got, err := c.SkillMarkdown(t.Context(), tt.source, tt.skill)
		if err != nil || got != tt.want {
			t.Errorf("SkillMarkdown(%q, %q) = %q, %v; want %q", tt.source, tt.skill, got, err, tt.want)
		}
	}

	errTests := []struct {
		source, skill, err string
	}{
		{"o/r", "missing", "skill not available on skills.sh"},
		{"o/r", "none", "skills.sh has no SKILL.md for o/r@none"},
		{"o/r", "down", "skills.sh download: 500 Internal Server Error"},
		{"o", "x", "invalid skills.sh skill"},
		{"o/r/x", "x", "invalid skills.sh skill"},
		{"/r", "x", "invalid skills.sh skill"},
		{"o/r", "", "invalid skills.sh skill"},
	}
	for _, tt := range errTests {
		_, err := c.SkillMarkdown(t.Context(), tt.source, tt.skill)
		if err == nil || !strings.Contains(err.Error(), tt.err) {
			t.Errorf("SkillMarkdown(%q, %q) error = %v, want %q", tt.source, tt.skill, err, tt.err)
		}
	}
}
