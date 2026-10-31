// Package skill reads, validates, hashes, discovers and copies skill directories.
// It works on the filesystem only and knows nothing about the library or agents.
package skill

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// FileName is the file that makes a directory a skill.
const FileName = "SKILL.md"

// Metadata is the validated frontmatter of a skill. Optional fields are empty
// when absent.
type Metadata struct {
	Name          string
	Description   string
	License       string
	Compatibility string
	AllowedTools  string            // "allowed-tools"
	Metadata      map[string]string // "metadata"
}

// Read parses and validates dir/SKILL.md (design §4). The frontmatter name must
// equal the directory name.
func Read(dir string) (Metadata, error) {
	m, err := ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return Metadata{}, err
	}
	if base := filepath.Base(dir); m.Name != base {
		return Metadata{}, fmt.Errorf("%s: name %q does not match directory name %q", FileName, m.Name, base)
	}
	return m, nil
}

// ReadFile parses and validates a SKILL.md file without checking the name of the
// directory that contains it. Used for candidates found in a source, whose
// directory may be renamed on install.
func ReadFile(path string) (Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, err
	}
	m, err := Parse(data)
	if err != nil {
		return Metadata{}, fmt.Errorf("%s: %w", FileName, err)
	}
	return m, nil
}

// Body returns the markdown of dir/SKILL.md without its frontmatter.
func Body(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return "", err
	}
	return StripFrontmatter(string(data)), nil
}

// StripFrontmatter removes a leading frontmatter block from markdown.
func StripFrontmatter(markdown string) string {
	_, body, err := splitFrontmatter(markdown)
	if err != nil {
		return markdown
	}
	// Drop leading blank lines, keeping the indentation of the first content line.
	for {
		line, rest, ok := strings.Cut(body, "\n")
		if strings.TrimSpace(line) != "" {
			return body
		}
		if !ok {
			return ""
		}
		body = rest
	}
}

// splitFrontmatter splits s into the frontmatter between a first line "---" and
// the next line "---", and the text after the closing line.
func splitFrontmatter(s string) (front, body string, err error) {
	first, rest, _ := strings.Cut(s, "\n")
	if strings.TrimSuffix(first, "\r") != "---" {
		return "", "", errors.New(`must start with a "---" frontmatter block`)
	}
	for i := 0; i < len(rest); {
		line, next := rest[i:], len(rest)
		if n := strings.IndexByte(line, '\n'); n >= 0 {
			line, next = line[:n], i+n+1
		}
		if strings.TrimSuffix(line, "\r") == "---" {
			return rest[:i], rest[next:], nil
		}
		i = next
	}
	return "", "", errors.New(`frontmatter is not closed by a "---" line`)
}

// Parse parses and validates the frontmatter of SKILL.md content (design §4).
func Parse(data []byte) (Metadata, error) {
	if !utf8.Valid(data) {
		return Metadata{}, errors.New("not valid UTF-8")
	}
	front, _, err := splitFrontmatter(string(data))
	if err != nil {
		return Metadata{}, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(front), &doc); err != nil {
		return Metadata{}, fmt.Errorf("invalid frontmatter: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return Metadata{}, errors.New("frontmatter must be a YAML mapping")
	}

	// Decoding into a yaml.Node does not reject duplicate keys, so check here.
	fields := map[string]*yaml.Node{}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		if _, dup := fields[k.Value]; dup {
			return Metadata{}, fmt.Errorf("duplicate field %q", k.Value)
		}
		fields[k.Value] = root.Content[i+1]
	}

	name, err := field(fields, "name", true)
	if err != nil {
		return Metadata{}, err
	}
	if err := validateName(name); err != nil {
		return Metadata{}, err
	}

	desc, err := field(fields, "description", true)
	if err != nil {
		return Metadata{}, err
	}
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return Metadata{}, errors.New("description must not be empty")
	}
	if n := utf8.RuneCountInString(desc); n > 1024 {
		return Metadata{}, fmt.Errorf("description must be at most 1024 characters, got %d", n)
	}

	m := Metadata{Name: name, Description: desc}
	if m.License, err = field(fields, "license", false); err != nil {
		return Metadata{}, err
	}
	if m.AllowedTools, err = field(fields, "allowed-tools", false); err != nil {
		return Metadata{}, err
	}
	if v, ok := fields["compatibility"]; ok {
		if m.Compatibility, err = stringValue("compatibility", v); err != nil {
			return Metadata{}, err
		}
		if n := utf8.RuneCountInString(m.Compatibility); n < 1 || n > 500 {
			return Metadata{}, fmt.Errorf("compatibility must be 1-500 characters, got %d", n)
		}
	}
	if v, ok := fields["metadata"]; ok {
		if v.Kind != yaml.MappingNode {
			return Metadata{}, fmt.Errorf("metadata must be a mapping, got %s", kind(v))
		}
		for i := 0; i+1 < len(v.Content); i += 2 {
			k := v.Content[i]
			if k.Kind != yaml.ScalarNode || k.ShortTag() != "!!str" {
				return Metadata{}, fmt.Errorf("metadata key %q must be a string, got %s", k.Value, kind(k))
			}
			s, err := stringValue("metadata."+k.Value, v.Content[i+1])
			if err != nil {
				return Metadata{}, err
			}
			if m.Metadata == nil {
				m.Metadata = map[string]string{}
			}
			m.Metadata[k.Value] = s
		}
	}
	return m, nil
}

// field returns the string value of a top-level frontmatter field.
func field(fields map[string]*yaml.Node, key string, required bool) (string, error) {
	v, ok := fields[key]
	if !ok {
		if required {
			return "", fmt.Errorf("missing required field %q", key)
		}
		return "", nil
	}
	return stringValue(key, v)
}

// stringValue returns the value of a node that must be a YAML string, so that
// "name: 123" or "name: true" is rejected rather than coerced.
func stringValue(key string, v *yaml.Node) (string, error) {
	if v.Kind != yaml.ScalarNode || v.ShortTag() != "!!str" {
		return "", fmt.Errorf("%s must be a string, got %s", key, kind(v))
	}
	return v.Value, nil
}

// kind names the YAML type of a node for error messages.
func kind(v *yaml.Node) string {
	switch v.Kind {
	case yaml.MappingNode:
		return "mapping"
	case yaml.SequenceNode:
		return "list"
	case yaml.AliasNode:
		return "alias"
	default:
		return strings.TrimPrefix(v.ShortTag(), "!!")
	}
}

func validateName(name string) error {
	invalid := func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-'
	}
	switch {
	case name == "":
		return errors.New("name must not be empty")
	case strings.ContainsFunc(name, invalid):
		return fmt.Errorf("name %q must be lowercase letters, digits and hyphens", name)
	case len(name) > 64:
		return fmt.Errorf("name must be at most 64 characters, got %d", len(name))
	case strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-"):
		return fmt.Errorf("name %q must not start or end with a hyphen", name)
	case strings.Contains(name, "--"):
		return fmt.Errorf("name %q must not contain consecutive hyphens", name)
	}
	return nil
}
