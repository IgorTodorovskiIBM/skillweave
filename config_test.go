package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGitHubURLBlob(t *testing.T) {
	repoURL, skillPath, err := ParseGitHubURL("https://github.com/example/repo/blob/main/skills/sample/SKILL.md")
	if err != nil {
		t.Fatalf("ParseGitHubURL returned error: %v", err)
	}
	if repoURL != "git@github.com:example/repo.git" {
		t.Fatalf("unexpected repo URL: %s", repoURL)
	}
	if skillPath != "skills/sample/SKILL.md" {
		t.Fatalf("unexpected skill path: %s", skillPath)
	}
}

func TestParseGitHubURLSlashBranchWithSkillsPath(t *testing.T) {
	repoURL, skillPath, err := ParseGitHubURL("https://github.com/example/repo/blob/feature/test/skills/sample/SKILL.md")
	if err != nil {
		t.Fatalf("ParseGitHubURL returned error: %v", err)
	}
	if repoURL != "git@github.com:example/repo.git" {
		t.Fatalf("unexpected repo URL: %s", repoURL)
	}
	if skillPath != "skills/sample/SKILL.md" {
		t.Fatalf("unexpected skill path: %s", skillPath)
	}
}

func TestParseGitHubURLRawSlashBranchWithSkillsPath(t *testing.T) {
	repoURL, skillPath, err := ParseGitHubURL("https://raw.githubusercontent.com/example/repo/feature/test/skills/sample/SKILL.md")
	if err != nil {
		t.Fatalf("ParseGitHubURL returned error: %v", err)
	}
	if repoURL != "git@github.com:example/repo.git" {
		t.Fatalf("unexpected repo URL: %s", repoURL)
	}
	if skillPath != "skills/sample/SKILL.md" {
		t.Fatalf("unexpected skill path: %s", skillPath)
	}
}

func TestParseGitHubURLEnterpriseSSH(t *testing.T) {
	repoURL, skillPath, err := ParseGitHubURL("git@github.ibm.com:itodorov/aivideoskills.git")
	if err != nil {
		t.Fatalf("ParseGitHubURL returned error: %v", err)
	}
	if repoURL != "git@github.ibm.com:itodorov/aivideoskills.git" {
		t.Fatalf("unexpected repo URL: %s", repoURL)
	}
	if skillPath != "" {
		t.Fatalf("expected empty skill path, got: %s", skillPath)
	}
}

func TestParseGitHubURLEnterpriseSSHNoSuffix(t *testing.T) {
	repoURL, _, err := ParseGitHubURL("git@github.ibm.com:itodorov/aivideoskills")
	if err != nil {
		t.Fatalf("ParseGitHubURL returned error: %v", err)
	}
	if repoURL != "git@github.ibm.com:itodorov/aivideoskills.git" {
		t.Fatalf("unexpected repo URL: %s", repoURL)
	}
}

func TestParseGitHubURLEnterpriseBlob(t *testing.T) {
	repoURL, skillPath, err := ParseGitHubURL("https://github.ibm.com/itodorov/aivideoskills/blob/main/skills/kokoro/SKILL.md")
	if err != nil {
		t.Fatalf("ParseGitHubURL returned error: %v", err)
	}
	if repoURL != "git@github.ibm.com:itodorov/aivideoskills.git" {
		t.Fatalf("unexpected repo URL: %s", repoURL)
	}
	if skillPath != "skills/kokoro/SKILL.md" {
		t.Fatalf("unexpected skill path: %s", skillPath)
	}
}

func TestParseLocalRepoArgWithRemote(t *testing.T) {
	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init")
	runGit("remote", "add", "origin", "git@github.ibm.com:itodorov/aivideoskills.git")

	repoURL, localPath, ok, err := ParseLocalRepoArg(dir)
	if err != nil {
		t.Fatalf("ParseLocalRepoArg returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true for a local directory")
	}
	if repoURL != "git@github.ibm.com:itodorov/aivideoskills.git" {
		t.Fatalf("expected remote URL, got: %s", repoURL)
	}
	// Resolve symlinks (macOS /var → /private/var) before comparing.
	wantRoot, _ := filepath.EvalSymlinks(dir)
	gotRoot, _ := filepath.EvalSymlinks(localPath)
	if gotRoot != wantRoot {
		t.Fatalf("expected local path %s, got: %s", wantRoot, gotRoot)
	}
}

func TestParseLocalRepoArgNoRemote(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "-C", dir, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	repoURL, localPath, ok, err := ParseLocalRepoArg(dir)
	if err != nil {
		t.Fatalf("ParseLocalRepoArg returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	want, _ := filepath.EvalSymlinks(dir)
	gotURL, _ := filepath.EvalSymlinks(repoURL)
	gotLocal, _ := filepath.EvalSymlinks(localPath)
	if gotURL != want || gotLocal != want {
		t.Fatalf("expected repo/local to be %s, got repo=%s local=%s", want, gotURL, gotLocal)
	}
}

func TestParseLocalRepoArgNotLocal(t *testing.T) {
	_, _, ok, err := ParseLocalRepoArg("owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("owner/repo should not be treated as a local path")
	}
}

func TestParseLocalRepoArgPlainDir(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o644)
	repoURL, localPath, ok, err := ParseLocalRepoArg(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true for an existing directory")
	}
	want, _ := filepath.EvalSymlinks(dir)
	gotURL, _ := filepath.EvalSymlinks(repoURL)
	gotLocal, _ := filepath.EvalSymlinks(localPath)
	if gotURL != want || gotLocal != want {
		t.Fatalf("expected repo/local to be %s, got repo=%s local=%s", want, gotURL, gotLocal)
	}
}

func TestRegisteredSkillToolDescriptionUsesStoredDescription(t *testing.T) {
	skill := RegisteredSkill{
		Name:        "sample",
		Description: "Custom guide summary",
	}
	if got := skill.ToolDescription(); got != "Custom guide summary" {
		t.Fatalf("unexpected tool description: %q", got)
	}
}

func TestRegisteredSkillToolDescriptionFallsBackToName(t *testing.T) {
	skill := RegisteredSkill{Name: "sample"}
	if got := skill.ToolDescription(); got != "Skill guide: sample" {
		t.Fatalf("unexpected tool description fallback: %q", got)
	}
}

func TestSkeletonSKILLIncludesDescription(t *testing.T) {
	got := SkeletonSKILL("sample", "Sample guide")
	if want := "description: \"Sample guide\""; !strings.Contains(got, want) {
		t.Fatalf("expected skeleton to contain %q, got %q", want, got)
	}
}
