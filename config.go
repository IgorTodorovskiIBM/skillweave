package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// RegisteredSkill is a skill tracked by the server.
type RegisteredSkill struct {
	Name        string `json:"name"`
	RepoURL     string `json:"repo_url"`
	SkillPath   string `json:"skill_path"`
	Description string `json:"description,omitempty"` // Stored MCP tool description when the document lacks frontmatter.
	LocalPath   string `json:"local_path,omitempty"`  // Local checkout root (e.g. /Users/igor/Projects/zos-porting)
}

// AICommand is a configured AI tool for merging learnings.
type AICommand struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"` // Static args; prompt is appended as the last arg.
}

// SkillConfig holds all registered skills and settings.
type SkillConfig struct {
	Skills     []RegisteredSkill `json:"skills"`
	AICommands []AICommand       `json:"ai_commands,omitempty"`
}

// FindAICommand looks up an AI command by name.
func (c *SkillConfig) FindAICommand(name string) (*AICommand, error) {
	for i := range c.AICommands {
		if c.AICommands[i].Name == name {
			return &c.AICommands[i], nil
		}
	}
	return nil, fmt.Errorf("AI command not configured: %q (use 'skillweave ai add' to add one)", name)
}

// AddAICommand adds or replaces an AI command by name.
func (c *SkillConfig) AddAICommand(cmd AICommand) {
	for i := range c.AICommands {
		if c.AICommands[i].Name == cmd.Name {
			c.AICommands[i] = cmd
			return
		}
	}
	c.AICommands = append(c.AICommands, cmd)
}

// RemoveAICommand removes an AI command by name. Returns true if found.
func (c *SkillConfig) RemoveAICommand(name string) bool {
	for i := range c.AICommands {
		if c.AICommands[i].Name == name {
			c.AICommands = append(c.AICommands[:i], c.AICommands[i+1:]...)
			return true
		}
	}
	return false
}

func configPath(cacheDir string) string {
	return filepath.Join(cacheDir, "skills.json")
}

// LoadConfig reads the skill registry from disk.
func LoadConfig(cacheDir string) (*SkillConfig, error) {
	logger := GetLogger().WithFields(map[string]interface{}{
		"cache_dir": cacheDir,
		"operation": "load_config",
	})

	path := configPath(cacheDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Debug("config file does not exist, returning empty config")
			return &SkillConfig{}, nil
		}
		logger.WithError(err).Error("failed to read config file")
		return nil, WrapError("read config", err)
	}

	var cfg SkillConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		logger.WithError(err).Error("failed to parse config file")
		return nil, WrapErrorWithFields("parse config", err, map[string]interface{}{
			"path": path,
		})
	}

	logger.WithFields(map[string]interface{}{
		"skill_count":   len(cfg.Skills),
		"ai_tool_count": len(cfg.AICommands),
	}).Debug("config loaded successfully")
	return &cfg, nil
}

// SaveConfig writes the skill registry to disk.
func SaveConfig(cacheDir string, cfg *SkillConfig) error {
	logger := GetLogger().WithFields(map[string]interface{}{
		"cache_dir":     cacheDir,
		"operation":     "save_config",
		"skill_count":   len(cfg.Skills),
		"ai_tool_count": len(cfg.AICommands),
	})
	logger.Debug("saving config")

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		logger.WithError(err).Error("failed to create cache directory")
		return WrapError("mkdir cache dir", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		logger.WithError(err).Error("failed to marshal config")
		return WrapError("marshal config", err)
	}

	path := configPath(cacheDir)
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		logger.WithError(err).Error("failed to write config file")
		return WrapErrorWithFields("write config", err, map[string]interface{}{
			"path": path,
		})
	}

	logger.Info("config saved successfully")
	return nil
}

// FindSkill looks up a registered skill by name.
func (c *SkillConfig) FindSkill(name string) (*RegisteredSkill, error) {
	for i := range c.Skills {
		if c.Skills[i].Name == name {
			return &c.Skills[i], nil
		}
	}
	return nil, fmt.Errorf("skill not registered: %q (use 'skillweave register' to add it)", name)
}

// AddSkill registers a new skill, replacing any existing one with the same name.
func (c *SkillConfig) AddSkill(s RegisteredSkill) {
	for i := range c.Skills {
		if c.Skills[i].Name == s.Name {
			c.Skills[i] = s
			return
		}
	}
	c.Skills = append(c.Skills, s)
}

// RemoveSkill removes a skill by name. Returns true if found.
func (c *SkillConfig) RemoveSkill(name string) bool {
	for i := range c.Skills {
		if c.Skills[i].Name == name {
			c.Skills = append(c.Skills[:i], c.Skills[i+1:]...)
			return true
		}
	}
	return false
}

// ToolDescription returns the configured tool description fallback.
func (s RegisteredSkill) ToolDescription() string {
	if desc := strings.TrimSpace(s.Description); desc != "" {
		return desc
	}
	return "Skill guide: " + s.Name
}

// githubBlobRe matches: https://<host>/<owner>/<repo>/blob/<branch>/<path>
// Host is generic so GitHub Enterprise (e.g. github.ibm.com) works too.
var githubBlobRe = regexp.MustCompile(`^https?://([\w.-]+)/([^/]+/[^/]+)/blob/[^/]+/(.+)$`)

// githubRawRe matches: https://raw.githubusercontent.com/<owner>/<repo>/<branch>/<path>
var githubRawRe = regexp.MustCompile(`^https://raw\.githubusercontent\.com/([^/]+/[^/]+)/[^/]+/(.+)$`)

// githubRepoRe matches: https://<host>/<owner>/<repo> (no path)
var githubRepoRe = regexp.MustCompile(`^https?://([\w.-]+)/([^/]+/[^/]+?)(?:\.git)?/?$`)

// sshRepoRe matches: git@<host>:<owner>/<repo>.git (host and nested paths generic)
var sshRepoRe = regexp.MustCompile(`^git@([\w.-]+):([^/]+/.+?)(?:\.git)?$`)

// shorthandRe matches: owner/repo (assumed to live on github.com)
var shorthandRe = regexp.MustCompile(`^([a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+)$`)

// ParseGitHubURL extracts repo URL and file path from various GitHub URL formats.
// Supported formats:
//   - https://github.com/owner/repo/blob/branch/path (blob URL)
//   - https://raw.githubusercontent.com/owner/repo/branch/path (raw URL)
//   - https://github.com/owner/repo (repo URL, path will be empty)
//   - git@github.com:owner/repo.git (SSH URL, path will be empty)
//   - owner/repo (shorthand, path will be empty)
func ParseGitHubURL(rawURL string) (repoURL, skillPath string, err error) {
	if repoURL, skillPath, ok := parseSkillURLWithKnownPrefixes(rawURL); ok {
		return repoURL, skillPath, nil
	}

	// Try blob URL first (most specific). Preserve the host for Enterprise.
	if m := githubBlobRe.FindStringSubmatch(rawURL); m != nil {
		return "git@" + m[1] + ":" + m[2] + ".git", m[3], nil
	}

	// Try raw.githubusercontent.com URL.
	if m := githubRawRe.FindStringSubmatch(rawURL); m != nil {
		return "git@github.com:" + m[1] + ".git", m[2], nil
	}

	// Try HTTPS repo URL (no file path). Preserve the host for Enterprise.
	if m := githubRepoRe.FindStringSubmatch(rawURL); m != nil {
		return "git@" + m[1] + ":" + m[2] + ".git", "", nil
	}

	// Try SSH repo URL. Preserve the host for Enterprise.
	if m := sshRepoRe.FindStringSubmatch(rawURL); m != nil {
		return "git@" + m[1] + ":" + m[2] + ".git", "", nil
	}

	// Try owner/repo shorthand.
	if m := shorthandRe.FindStringSubmatch(rawURL); m != nil {
		return "git@github.com:" + m[1] + ".git", "", nil
	}

	if repoURL, ok := parseGitHubRepoOnly(rawURL); ok {
		return repoURL, "", nil
	}

	return "", "", fmt.Errorf("unrecognized URL format: %s\nSupported formats:\n  https://github.com/owner/repo/blob/branch/path-to-SKILL.md\n  https://github.com/owner/repo\n  git@github.com:owner/repo.git   (any host, e.g. git@github.ibm.com:owner/repo.git)\n  owner/repo\n  .  or  /path/to/local/repo   (a local git checkout; use --path for the SKILL.md)", rawURL)
}

func parseSkillURLWithKnownPrefixes(rawURL string) (string, string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", false
	}

	var repo string
	var tail string
	switch u.Host {
	case "github.com":
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 5 || parts[2] != "blob" {
			return "", "", false
		}
		repo = "git@github.com:" + parts[0] + "/" + strings.TrimSuffix(parts[1], ".git") + ".git"
		tail = strings.Join(parts[3:], "/")
	case "raw.githubusercontent.com":
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 4 {
			return "", "", false
		}
		repo = "git@github.com:" + parts[0] + "/" + strings.TrimSuffix(parts[1], ".git") + ".git"
		tail = strings.Join(parts[2:], "/")
	default:
		return "", "", false
	}

	for _, marker := range []string{".codex/skills/", "skills/"} {
		if idx := strings.Index(tail, marker); idx >= 0 {
			return repo, tail[idx:], true
		}
	}
	return "", "", false
}

func parseGitHubRepoOnly(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}

	switch u.Host {
	case "github.com":
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 {
			return "", false
		}
		return "git@github.com:" + parts[0] + "/" + strings.TrimSuffix(parts[1], ".git") + ".git", true
	case "raw.githubusercontent.com":
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 {
			return "", false
		}
		return "git@github.com:" + parts[0] + "/" + strings.TrimSuffix(parts[1], ".git") + ".git", true
	default:
		return "", false
	}
}

// DeriveSkillName guesses a short name from the skill path.
// "skills/zos-porting-cli/SKILL.md" → "zos-porting-cli"
// "SKILL.md" → "default"
func DeriveSkillName(skillPath string) string {
	dir := filepath.Dir(skillPath)
	if dir == "." || dir == "" {
		return "default"
	}
	return filepath.Base(dir)
}

// parseFrontmatter extracts name and description from a YAML frontmatter block
// delimited by "---" lines. Returns (name, description, body).
func parseFrontmatter(raw string) (string, string, string) {
	const delim = "---"

	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, delim) {
		return "", "", raw
	}

	rest := trimmed[len(delim):]
	idx := strings.Index(rest, "\n"+delim)
	if idx < 0 {
		return "", "", raw
	}

	frontmatter := rest[:idx]
	body := strings.TrimSpace(rest[idx+len("\n"+delim):])

	var name, desc string
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colonIdx])
		val := strings.TrimSpace(line[colonIdx+1:])
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}
		switch key {
		case "name":
			name = val
		case "description":
			desc = val
		}
	}
	return name, desc, body
}

// looksLikeLocalPath reports whether arg should be treated as a local
// filesystem path rather than a remote URL or owner/repo shorthand.
func looksLikeLocalPath(arg string) bool {
	if arg == "." || arg == ".." {
		return true
	}
	if strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "./") ||
		strings.HasPrefix(arg, "../") || strings.HasPrefix(arg, "~") {
		return true
	}
	// A bare name (no slash) that exists as a directory, e.g. "myrepo".
	// Anything with a slash is left for owner/repo shorthand handling.
	if !strings.Contains(arg, "/") {
		if info, err := os.Stat(arg); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// expandHome expands a leading ~ to the user's home directory.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// ParseLocalRepoArg resolves a local filesystem argument into a repo URL and
// local checkout path. When the directory is inside a git repo with an
// "origin" remote, that remote becomes the repo URL (so pushes/PRs target it)
// and the worktree root becomes the local path. A repo without a remote (or a
// plain directory) falls back to using the directory itself as the repo URL,
// which git can still clone from locally. Returns ok=false if arg is not a
// local path or the path doesn't exist.
func ParseLocalRepoArg(arg string) (repoURL, localPath string, ok bool, err error) {
	if !looksLikeLocalPath(arg) {
		return "", "", false, nil
	}

	abs, err := filepath.Abs(expandHome(arg))
	if err != nil {
		return "", "", true, fmt.Errorf("resolve %q: %w", arg, err)
	}
	info, statErr := os.Stat(abs)
	if statErr != nil || !info.IsDir() {
		return "", "", true, fmt.Errorf("not a directory: %s", abs)
	}

	root := gitRepoRoot(abs)
	if root == "" {
		// Not a git checkout — use the directory as-is.
		return abs, abs, true, nil
	}

	if remote := gitRemoteURL(root); remote != "" {
		return remote, root, true, nil
	}
	// Git repo with no remote — clone from the local path directly.
	return root, root, true, nil
}

// DetectLocalPath checks if the current directory (or a parent) is a git
// checkout whose remote matches repoURL. Returns the repo root or "".
func DetectLocalPath(repoURL string) string {
	// Normalize the repo URL for comparison — strip .git suffix and protocol.
	norm := normalizeRepoURL(repoURL)

	// Walk up from cwd looking for a .git directory.
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			// Found a git repo — check its remotes.
			out, err := exec.Command("git", "-C", dir, "remote", "-v").Output()
			if err == nil {
				for _, line := range strings.Split(string(out), "\n") {
					if strings.Contains(normalizeRepoURL(line), norm) {
						return dir
					}
				}
			}
			// Found a git repo but remotes don't match — stop looking.
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// normalizeRepoURL strips protocol, .git suffix, and common prefixes for comparison.
func normalizeRepoURL(u string) string {
	u = strings.ToLower(u)
	// Strip protocol
	for _, prefix := range []string{"https://", "http://", "git@", "ssh://"} {
		u = strings.TrimPrefix(u, prefix)
	}
	// git@github.com:user/repo → github.com/user/repo
	u = strings.Replace(u, ":", "/", 1)
	u = strings.TrimSuffix(u, ".git")
	return u
}

// SkeletonSKILL returns a minimal SKILL.md template for a new skill.
func SkeletonSKILL(name, description string) string {
	return fmt.Sprintf(`---
name: %s
description: %q
---

# %s

<!-- This is a new skill. Start capturing learnings and push when ready. -->
`, name, description, name)
}

// FormatSkillList returns a human-readable list of registered skills.
func FormatSkillList(cfg *SkillConfig) string {
	if len(cfg.Skills) == 0 {
		return "No skills registered. Use 'skillweave register <github-url>' to add one."
	}
	var sb strings.Builder
	for i, s := range cfg.Skills {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("  %s\n    repo:  %s\n    path:  %s", s.Name, s.RepoURL, s.SkillPath))
		if s.LocalPath != "" {
			sb.WriteString(fmt.Sprintf("\n    local: %s", filepath.Join(s.LocalPath, s.SkillPath)))
		}
	}
	return sb.String()
}
