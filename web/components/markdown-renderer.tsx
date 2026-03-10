"use client";

import {
  useEffect,
  useRef,
  useId,
  useState,
  useCallback,
  type ReactNode,
  type ComponentPropsWithoutRef,
} from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import mermaid from "mermaid";

// Initialize mermaid with sensible defaults (once, at module load)
mermaid.initialize({
  startOnLoad: false,
  theme: "default",
  // Notes render in the main document, so keep Mermaid's HTML/click features
  // disabled instead of reopening an unsandboxed injection surface.
  securityLevel: "strict",
});

// ── Language display names ──────────────────────────────────────────────────
const LANG_LABELS: Record<string, string> = {
  js: "JavaScript",
  jsx: "JavaScript (JSX)",
  ts: "TypeScript",
  tsx: "TypeScript (TSX)",
  py: "Python",
  python: "Python",
  rb: "Ruby",
  go: "Go",
  rs: "Rust",
  sh: "Shell",
  bash: "Bash",
  zsh: "Zsh",
  sql: "SQL",
  json: "JSON",
  yaml: "YAML",
  yml: "YAML",
  css: "CSS",
  html: "HTML",
  xml: "XML",
  md: "Markdown",
  toml: "TOML",
  dockerfile: "Dockerfile",
  graphql: "GraphQL",
  c: "C",
  cpp: "C++",
  java: "Java",
  kt: "Kotlin",
  swift: "Swift",
  r: "R",
};

/**
 * Returns a human-friendly label for a language identifier.
 * Falls back to the raw identifier with its first letter capitalised.
 */
function langLabel(id: string): string {
  return LANG_LABELS[id] ?? id.charAt(0).toUpperCase() + id.slice(1);
}

// ── Copy button ─────────────────────────────────────────────────────────────

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  const copyTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (copyTimeoutRef.current) {
        clearTimeout(copyTimeoutRef.current);
      }
    };
  }, []);

  const handleCopy = useCallback(async () => {
    if (!navigator.clipboard?.writeText) {
      return;
    }

    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      if (copyTimeoutRef.current) {
        clearTimeout(copyTimeoutRef.current);
      }
      copyTimeoutRef.current = setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard access can be unavailable or denied by browser policy.
    }
  }, [text]);

  return (
    <button
      type="button"
      onClick={() => {
        void handleCopy();
      }}
      aria-label="Copy code"
      className="rounded px-2 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-border hover:text-foreground"
    >
      {copied ? "Copied!" : "Copy"}
    </button>
  );
}

// ── Mermaid diagram ─────────────────────────────────────────────────────────

/**
 * Renders a mermaid diagram from its chart definition string.
 *
 * Uses the mermaid library to render SVG on the client side.
 */
function MermaidDiagram({ chart }: { chart: string }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const uniqueId = useId().replace(/:/g, "-");

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    let cancelled = false;
    mermaid
      .render(`mermaid${uniqueId}`, chart.trim())
      .then(({ svg }) => {
        if (!cancelled && el) {
          el.innerHTML = svg;
        }
      })
      .catch(() => {
        // If mermaid fails to parse, show the raw text as a code block
        if (!cancelled && el) {
          el.textContent = chart;
        }
      });

    return () => {
      cancelled = true;
    };
  }, [chart, uniqueId]);

  return (
    <div
      ref={containerRef}
      data-mermaid-diagram=""
      className="my-3 flex justify-center overflow-x-auto"
    />
  );
}

// ── Code block with header bar ──────────────────────────────────────────────

/**
 * Extracts the plain text content from a React node tree.
 */
function extractText(node: ReactNode): string {
  if (typeof node === "string") return node;
  if (typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(extractText).join("");
  if (node && typeof node === "object" && "props" in node) {
    return extractText(
      (node as { props: { children?: ReactNode } }).props.children
    );
  }
  return "";
}

/**
 * Full-featured markdown renderer using react-markdown with remark-gfm,
 * rehype-highlight (syntax highlighting), and mermaid diagram support.
 *
 * Code blocks include a header bar showing the language and a copy button.
 *
 * Supports: headings, bold, italic, strikethrough, inline code, code blocks
 * (with syntax highlighting and mermaid), tables, lists, task lists, links,
 * images, blockquotes, horizontal rules, and mermaid diagrams.
 */
export function MarkdownRenderer({ content }: { content: string }) {
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      rehypePlugins={[rehypeHighlight]}
      components={{
        // Intercept <pre> to wrap code blocks with a header bar.
        pre({ children, ...rest }) {
          // ReactMarkdown wraps <code> inside <pre> for fenced blocks.
          // Extract language from the inner <code> className.
          const codeChild =
            children &&
            typeof children === "object" &&
            "props" in children &&
            (children as { props: { className?: string; children?: ReactNode } });

          const className = codeChild ? codeChild.props.className : undefined;
          const langMatch = /language-(\w+)/.exec(className ?? "");
          const lang = langMatch?.[1];

          // Mermaid: render as diagram, not as a code block
          if (lang === "mermaid" && codeChild) {
            const chart = extractText(codeChild.props.children);
            return <MermaidDiagram chart={chart.replace(/\n$/, "")} />;
          }

          // Regular code block — add header bar with language + copy button
          if (lang) {
            const codeText = extractText(
              codeChild ? codeChild.props.children : children
            );
            return (
              <div className="not-prose my-4 overflow-hidden rounded-lg border border-border">
                <div className="flex items-center justify-between border-b border-border bg-muted px-4 py-1.5">
                  <span className="text-xs font-medium text-muted-foreground">
                    {langLabel(lang)}
                  </span>
                  <CopyButton text={codeText.replace(/\n$/, "")} />
                </div>
                <pre
                  {...rest}
                  className="!m-0 !rounded-none !border-0"
                >
                  {children}
                </pre>
              </div>
            );
          }

          // No language specified — plain pre block
          return <pre {...rest}>{children}</pre>;
        },

        // Handle inline code (no change needed, but keep mermaid guard)
        code(props: ComponentPropsWithoutRef<"code"> & { inline?: boolean }) {
          // eslint-disable-next-line @typescript-eslint/no-unused-vars
          const { className, children, inline, ...rest } = props;
          return (
            <code className={className} {...rest}>
              {children}
            </code>
          );
        },
      }}
    >
      {content}
    </ReactMarkdown>
  );
}
