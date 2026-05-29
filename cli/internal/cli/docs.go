package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

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
			_, _ = io.WriteString(stdout, content)
			return nil
		},
	}
	cmd.AddCommand(newDocsSearchCommand(stdout))
	return cmd
}

func writeDocsTopicList(stdout io.Writer) error {
	// docs.Topics() is always non-empty: the topics are compiled in via go:embed.
	_, _ = fmt.Fprintln(stdout, "Available docs topics (use `pc docs <topic>`):")
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, topic := range docs.Topics() {
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", topic.Name, topic.Title)
	}
	return tw.Flush()
}

func newDocsSearchCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Search the embedded documentation",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			hits := docs.Search(query)
			if len(hits) == 0 {
				_, _ = fmt.Fprintf(stdout, "No documentation matches for %q.\n", query)
				return nil
			}
			for _, hit := range hits {
				_, _ = fmt.Fprintf(stdout, "%s: %s\n", hit.Topic, hit.Heading)
				if hit.Excerpt != "" {
					_, _ = fmt.Fprintf(stdout, "    %s\n", hit.Excerpt)
				}
			}
			return nil
		},
	}
}
