package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/conn-castle/personal-context/cli/internal/docs"
	"github.com/spf13/cobra"
)

func newDocsCommand(stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs [topic]",
		Short: "Show embedded Personal Context reference documentation",
		Long: "Print embedded concept documentation that matches the installed binary.\n" +
			"Run `pc docs` to list topics, `pc docs <topic>` to print one, and\n" +
			"`pc docs search <query>` to find matching sections.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return writeDocsTopicList(stdout)
			}
			content, err := docs.Get(args[0])
			if err != nil {
				return err
			}
			// Embedded topic files always end with a trailing newline.
			_, err = io.WriteString(stdout, content)
			return err
		},
	}
	cmd.AddCommand(newDocsSearchCommand(stdout))
	return cmd
}

func writeDocsTopicList(stdout io.Writer) error {
	// docs.Topics() is always non-empty: the topics are compiled in via go:embed.
	var out strings.Builder
	out.WriteString("Available docs topics (use `pc docs <topic>`):\n")
	for _, topic := range docs.Topics() {
		_, _ = fmt.Fprintf(&out, "  %-24s %s\n", topic.Name, topic.Title)
	}
	_, err := io.WriteString(stdout, out.String())
	return err
}

func newDocsSearchCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Search the embedded documentation",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			hits := docs.Search(query)
			var out strings.Builder
			if len(hits) == 0 {
				_, _ = fmt.Fprintf(&out, "No documentation matches for %q.\n", query)
				_, err := io.WriteString(stdout, out.String())
				return err
			}
			for _, hit := range hits {
				_, _ = fmt.Fprintf(&out, "%s: %s\n", hit.Topic, hit.Heading)
				if hit.Excerpt != "" {
					_, _ = fmt.Fprintf(&out, "    %s\n", hit.Excerpt)
				}
			}
			_, err := io.WriteString(stdout, out.String())
			return err
		},
	}
}
