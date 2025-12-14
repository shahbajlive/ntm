package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ProjectConfig represents the structure of .ntm/config.toml
type ProjectConfig struct {
	Defaults     ProjectDefaults  `toml:"defaults"`
	Palette      ProjectPalette   `toml:"palette"`
	PaletteState PaletteState     `toml:"palette_state"`
	Templates    ProjectTemplates `toml:"templates"`
	Agents       AgentConfig      `toml:"agents"`
}

// ProjectDefaults holds default settings for the project
type ProjectDefaults struct {
	Agents map[string]int `toml:"agents"` // e.g., { cc = 2, cod = 1 }
}

// ProjectPalette holds palette configuration
type ProjectPalette struct {
	File string `toml:"file"` // Path to palette.md relative to .ntm/
}

// ProjectTemplates holds template configuration
type ProjectTemplates struct {
	Dir string `toml:"dir"` // Path to templates dir relative to .ntm/
}

// FindProjectConfig searches for .ntm/config.toml starting from dir and going up.
// Returns the directory containing .ntm/ and the loaded config.
func FindProjectConfig(startDir string) (string, *ProjectConfig, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", nil, err
	}

	for {
		configPath := filepath.Join(dir, ".ntm", "config.toml")
		if info, err := os.Stat(configPath); err == nil && !info.IsDir() {
			cfg, err := LoadProjectConfig(configPath)
			return dir, cfg, err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil, nil // Reached root, no config found
		}
		dir = parent
	}
}

// LoadProjectConfig loads a project configuration from a file
func LoadProjectConfig(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg ProjectConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing project config: %w", err)
	}

	return &cfg, nil
}

// InitProjectConfig initializes .ntm configuration for the current directory.
// If force is true, it overwrites .ntm/config.toml if it already exists.
func InitProjectConfig(force bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	ntmDir := filepath.Join(cwd, ".ntm")
	if err := os.MkdirAll(ntmDir, 0755); err != nil {
		return fmt.Errorf("creating .ntm directory: %w", err)
	}

	// Create config.toml
	configPath := filepath.Join(ntmDir, "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		if !force {
			return fmt.Errorf("project config already exists at %s", configPath)
		}
	}

	content := `# Project-specific NTM configuration
# Overrides global settings for this project

[defaults]
# agents = { cc = 2, cod = 1 }

[palette]
# file = "palette.md"  # Relative to .ntm/

[palette_state]
# pinned = ["build", "test"]
# favorites = ["build"]

[templates]
# dir = "templates"    # Relative to .ntm/

[agents]
# claude = "claude --project ..."
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing config.toml: %w", err)
	}

	// Create palette.md scaffold
	palettePath := filepath.Join(ntmDir, "palette.md")
	if _, err := os.Stat(palettePath); os.IsNotExist(err) {
		paletteContent := `# Project Commands

## Project
### build | Build Project
make build

### test | Run Tests
go test ./...
`
		if err := os.WriteFile(palettePath, []byte(paletteContent), 0644); err != nil {
			return fmt.Errorf("writing palette.md: %w", err)
		}
	}

	// Create templates dir
	if err := os.MkdirAll(filepath.Join(ntmDir, "templates"), 0755); err != nil {
		return fmt.Errorf("creating templates dir: %w", err)
	}

	fmt.Printf("Initialized project config in %s\n", ntmDir)
	return nil
}
