package cli

import "github.com/conn-castle/personal-context/cli/internal/repository"

// builtinTemplates defines the templates seeded by `pc setup`.
var builtinTemplates = []repository.CreateTemplateInput{
	{
		Name:        "text-only",
		HTMLContent: textOnlyTemplateHTML,
		Description: strPtr("Plain text slide with a heading and body."),
	},
	{
		Name:        "single-image",
		HTMLContent: singleImageTemplateHTML,
		Description: strPtr("Slide with a single centered image and optional caption."),
	},
}

func strPtr(s string) *string { return &s }

const textOnlyTemplateHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  body { font-family: system-ui, sans-serif; margin: 2rem; color: #1a1a1a; }
  h1 { font-size: 2rem; margin-bottom: 1rem; }
  .body { font-size: 1.25rem; line-height: 1.6; }
</style>
</head>
<body>
  <h1>Title</h1>
  <div class="body"><p>Content goes here.</p></div>
</body>
</html>`

const singleImageTemplateHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  body { font-family: system-ui, sans-serif; margin: 2rem; color: #1a1a1a; text-align: center; }
  img { max-width: 100%; max-height: 80vh; object-fit: contain; }
  .caption { font-size: 1rem; color: #555; margin-top: 0.5rem; }
</style>
</head>
<body>
  <img src="figures/image.png" alt="Image">
  <div class="caption">Caption</div>
</body>
</html>`
