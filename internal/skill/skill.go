package skill

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Skill represents an Agent Skill in the AgentSkills format
type Skill struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
	Author      string `yaml:"author"`
	Metadata    struct {
		Agent struct {
			AgentAgnostic bool   `yaml:"agent_agnostic"`
			Emoji         string `yaml:"emoji"`
			Scripts       struct {
				PreSpawn []string `yaml:"pre_spawn"`
			} `yaml:"scripts"`
			Tools       []string `yaml:"tools"`
			Channels    []string `yaml:"channels"`
			Permissions struct {
				Allow []string `yaml:"allow"`
			} `yaml:"permissions"`
			Workflow []struct {
				Step        string `yaml:"step"`
				Description string `yaml:"description"`
				Tool        string `yaml:"tool,omitempty"`
			} `yaml:"workflow"`
			Install []struct {
				ID      string `yaml:"id"`
				Kind    string `yaml:"kind"`
				Package string `yaml:"package"`
			} `yaml:"install"`
			Requires struct {
				Bins []string `yaml:"bins"`
				Env  []string `yaml:"env"`
			} `yaml:"requires"`
			NPMPackage string `yaml:"npm_package"`
		} `yaml:"agent"`
	} `yaml:"metadata"`
	Path        string `yaml:"-"`
	Instruction string `yaml:"-"`
}

// LoadSkill loads a skill from a directory
func LoadSkill(name string) (*Skill, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// Potential skill locations
	paths := []string{
		filepath.Join(home, ".agent", "skills", name),
		filepath.Join(".agent", "skills", name),
	}

	var skillDir string
	for _, p := range paths {
		if _, err := os.Stat(filepath.Join(p, "SKILL.md")); err == nil {
			skillDir = p
			break
		}
	}

	if skillDir == "" {
		return nil, fmt.Errorf("skill %q not found or missing SKILL.md", name)
	}

	return LoadFromPath(skillDir)
}

// LoadFromPath loads a skill from an absolute path
func LoadFromPath(path string) (*Skill, error) {
	skillFile := filepath.Join(path, "SKILL.md")
	data, err := os.ReadFile(skillFile)
	if err != nil {
		return nil, err
	}

	manifestFile := filepath.Join(path, "manifest.yaml")
	manifestData, manifestErr := os.ReadFile(manifestFile)

	// Split frontmatter and body from SKILL.md
	parts := bytes.SplitN(data, []byte("---\n"), 3)
	if len(parts) < 3 {
		// Try with \r\n
		parts = bytes.SplitN(data, []byte("---\r\n"), 3)
	}

	var sk Skill
	if len(parts) >= 3 {
		if err := yaml.Unmarshal(parts[1], &sk); err != nil {
			return nil, fmt.Errorf("failed to parse skill frontmatter: %w", err)
		}
		sk.Instruction = string(parts[2])
	} else {
		// No frontmatter in SKILL.md, treat entire file as instruction
		sk.Instruction = string(data)
	}

	// If manifest.yaml exists, it takes precedence for technical metadata
	if manifestErr == nil {
		var manifestSk Skill
		if err := yaml.Unmarshal(manifestData, &manifestSk); err == nil {
			// Merge manifest into sk
			if manifestSk.Name != "" {
				sk.Name = manifestSk.Name
			}
			if manifestSk.Description != "" {
				sk.Description = manifestSk.Description
			}
			if manifestSk.Version != "" {
				sk.Version = manifestSk.Version
			}
			if manifestSk.Author != "" {
				sk.Author = manifestSk.Author
			}
			sk.Metadata = manifestSk.Metadata
		}
	}

	sk.Path = path
	return &sk, nil
}

// RunPreSpawnScripts executes the pre-spawn scripts for the skill
func (s *Skill) RunPreSpawnScripts(agentType string) error {
	for _, scriptRelPath := range s.Metadata.Agent.Scripts.PreSpawn {
		scriptPath := filepath.Join(s.Path, scriptRelPath)
		if _, err := os.Stat(scriptPath); err != nil {
			continue // Skip if script doesn't exist
		}

		cmd := exec.Command("bash", scriptPath, agentType)
		cmd.Dir = s.Path
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: pre_spawn script %s failed: %v\n", scriptRelPath, err)
		}
	}
	return nil
}

// ListSkills returns a list of all installed skills
func ListSkills() ([]*Skill, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	skillsDir := filepath.Join(home, ".agent", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Skill{}, nil
		}
		return nil, err
	}

	var skills []*Skill
	for _, entry := range entries {
		if entry.IsDir() {
			sk, err := LoadFromPath(filepath.Join(skillsDir, entry.Name()))
			if err == nil {
				skills = append(skills, sk)
			}
		}
	}

	return skills, nil
}
