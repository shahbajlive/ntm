package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/shahbajlive/ntm/internal/output"
	"github.com/shahbajlive/ntm/internal/skill"
	"github.com/spf13/cobra"
)

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage agent skills",
		Long:  `List and inspect agent skills (SKILL.md).`,
	}

	cmd.AddCommand(newSkillListCmd())
	cmd.AddCommand(newSkillInfoCmd())

	return cmd
}

func newSkillListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all installed skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillList()
		},
	}
	return cmd
}

func runSkillList() error {
	skills, err := skill.ListSkills()
	if err != nil {
		return err
	}

	if IsJSONOutput() {
		return output.PrintJSON(skills)
	}

	if len(skills) == 0 {
		fmt.Println("No skills found. Skills should be in ~/.agent/skills/<skill-name>/SKILL.md")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tAUTHOR\tDESCRIPTION")
	for _, sk := range skills {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", sk.Name, sk.Version, sk.Author, sk.Description)
	}
	w.Flush()
	return nil
}

func newSkillInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <skill-name>",
		Short: "Show detailed information about a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillInfo(args[0])
		},
	}
	return cmd
}

func runSkillInfo(name string) error {
	sk, err := skill.LoadSkill(name)
	if err != nil {
		return err
	}

	if IsJSONOutput() {
		return output.PrintJSON(sk)
	}

	fmt.Printf("Skill: %s\n", sk.Name)
	fmt.Printf("Description: %s\n", sk.Description)
	fmt.Printf("Version: %s\n", sk.Version)
	fmt.Printf("Author: %s\n", sk.Author)
	fmt.Printf("Path: %s\n", sk.Path)
	fmt.Printf("\nPre-spawn scripts:\n")
	if len(sk.Metadata.Agent.Scripts.PreSpawn) > 0 {
		for _, s := range sk.Metadata.Agent.Scripts.PreSpawn {
			fmt.Printf("  - %s\n", s)
		}
	} else {
		fmt.Println("  (none)")
	}
	fmt.Printf("\nInstruction set:\n")
	fmt.Println("--------------------------------------------------")
	fmt.Println(sk.Instruction)
	fmt.Println("--------------------------------------------------")

	return nil
}
