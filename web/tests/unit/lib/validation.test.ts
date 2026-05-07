import { describe, expect, it } from "vitest";
import {
  isValidSlideId,
  isValidGitHash,
  isValidDate,
  isValidISOTimestamp,
  parseQueryInt,
  isValidFilename,
  normalizeNotes,
  validateSlideUpdateInput,
} from "@/lib/validation";

describe("isValidSlideId", () => {
  it.each([
    ["20250304-a3f2b7e1", true],
    ["20261231-00000000", true],
    ["20250304-ffffffff", true],
    ["20250304-ABCDEF01", false],
    ["2025030-a3f2b7e1", false],
    ["20250304-a3f2b7e", false],
    ["", false],
    ["not-a-slide-id", false],
    ["20250304_a3f2b7e1", false],
  ])("isValidSlideId(%s) => %s", (input, expected) => {
    expect(isValidSlideId(input)).toBe(expected);
  });
});

describe("isValidGitHash", () => {
  it.each([
    ["a".repeat(40), true],
    ["1234567890abcdef1234567890abcdef12345678", true],
    ["A".repeat(40), false],
    ["a".repeat(39), false],
    ["a".repeat(41), false],
    ["g".repeat(40), false],
    ["", false],
  ])("isValidGitHash(%s) => %s", (input, expected) => {
    expect(isValidGitHash(input)).toBe(expected);
  });
});

describe("isValidDate", () => {
  it.each([
    ["2025-03-04", true],
    ["2026-12-31", true],
    ["2025-02-28", true],
    ["2024-02-29", true],
    ["2025-02-29", false],
    ["2025-13-01", false],
    ["2025-00-01", false],
    ["2025-01-32", false],
    ["not-a-date", false],
    ["", false],
    ["20250304", false],
  ])("isValidDate(%s) => %s", (input, expected) => {
    expect(isValidDate(input)).toBe(expected);
  });
});

describe("isValidISOTimestamp", () => {
  it.each([
    ["2025-03-04T14:32:00Z", true],
    ["2025-03-04T14:32:00.000Z", true],
    ["2025-03-04T14:32:00+00:00", true],
    ["not-a-timestamp", false],
    ["", false],
  ])("isValidISOTimestamp(%s) => %s", (input, expected) => {
    expect(isValidISOTimestamp(input)).toBe(expected);
  });
});

describe("parseQueryInt", () => {
  it("returns default for null", () => {
    expect(parseQueryInt(null, 20)).toBe(20);
  });

  it("returns default for empty string", () => {
    expect(parseQueryInt("", 20)).toBe(20);
  });

  it("returns default for NaN", () => {
    expect(parseQueryInt("abc", 20)).toBe(20);
  });

  it("parses valid integer", () => {
    expect(parseQueryInt("10", 20)).toBe(10);
  });

  it("clamps to min", () => {
    expect(parseQueryInt("0", 20, 1)).toBe(1);
  });

  it("clamps to max", () => {
    expect(parseQueryInt("200", 20, 1, 100)).toBe(100);
  });

  it("allows value within range", () => {
    expect(parseQueryInt("50", 20, 1, 100)).toBe(50);
  });

  it("handles negative values with min", () => {
    expect(parseQueryInt("-5", 20, 0)).toBe(0);
  });
});

describe("isValidFilename", () => {
  it.each([
    ["image.png", true],
    ["loss-curve.png", true],
    ["file with spaces.txt", true],
    ["file..txt", true],
    ["", false],
    ["path/to/file.png", false],
    ["back\\slash.txt", false],
    ["..", false],
    [".", false],
    ["file\0name.txt", false],
  ])("isValidFilename(%s) => %s", (input, expected) => {
    expect(isValidFilename(input)).toBe(expected);
  });
});

describe("normalizeNotes", () => {
  it("returns null for null", () => {
    expect(normalizeNotes(null)).toBeNull();
  });

  it("returns null for undefined", () => {
    expect(normalizeNotes(undefined)).toBeNull();
  });

  it("returns null for empty string", () => {
    expect(normalizeNotes("")).toBeNull();
  });

  it("returns the string for non-empty", () => {
    expect(normalizeNotes("hello")).toBe("hello");
  });

  it("preserves whitespace-only strings", () => {
    expect(normalizeNotes("  ")).toBe("  ");
  });
});

describe("validateSlideUpdateInput", () => {
  it("rejects empty body", () => {
    const result = validateSlideUpdateInput({});
    expect(result.valid).toBe(false);
    if (!result.valid) expect(result.error).toContain("at least one field");
  });

  it("rejects unknown fields", () => {
    const result = validateSlideUpdateInput({ unknown: "value" });
    expect(result.valid).toBe(false);
    if (!result.valid) expect(result.error).toContain("unknown");
  });

  it("accepts valid project_id update", () => {
    const result = validateSlideUpdateInput({ project_id: "org/project" });
    expect(result.valid).toBe(true);
  });

  it("rejects null project_id", () => {
    const result = validateSlideUpdateInput({ project_id: null });
    expect(result.valid).toBe(false);
  });

  it("rejects empty string project_id", () => {
    const body: Record<string, unknown> = { project_id: "" };
    const result = validateSlideUpdateInput(body);
    expect(result.valid).toBe(false);
    expect(body.project_id).toBe("");
  });

  it("rejects non-string project_id", () => {
    const result = validateSlideUpdateInput({
      project_id: 123 as unknown as string,
    });
    expect(result.valid).toBe(false);
  });

  it("normalizes empty string notes to null in returned data", () => {
    const body: Record<string, unknown> = { notes: "" };
    const result = validateSlideUpdateInput(body);
    expect(result.valid).toBe(true);
    if (result.valid) {
      expect(result.data.notes).toBeNull();
      // Original body must not be mutated
      expect(body.notes).toBe("");
    }
  });

  it("accepts valid notes update", () => {
    const result = validateSlideUpdateInput({ notes: "some notes" });
    expect(result.valid).toBe(true);
    if (result.valid) {
      expect(result.data.notes).toBe("some notes");
    }
  });

  it("accepts null notes", () => {
    const result = validateSlideUpdateInput({ notes: null });
    expect(result.valid).toBe(true);
    if (result.valid) {
      expect(result.data.notes).toBeNull();
    }
  });

  it("rejects non-string notes", () => {
    const result = validateSlideUpdateInput({
      notes: 123 as unknown as string,
    });
    expect(result.valid).toBe(false);
  });

  it("accepts valid git_hash", () => {
    const result = validateSlideUpdateInput({ git_hash: "a".repeat(40) });
    expect(result.valid).toBe(true);
  });

  it("rejects invalid git_hash", () => {
    const result = validateSlideUpdateInput({ git_hash: "short" });
    expect(result.valid).toBe(false);
    if (!result.valid) expect(result.error).toContain("40-character hex");
  });

  it("accepts null git_hash", () => {
    const result = validateSlideUpdateInput({ git_hash: null });
    expect(result.valid).toBe(true);
  });

  it("accepts valid git_remote_url with https://", () => {
    const result = validateSlideUpdateInput({
      git_remote_url: "https://github.com/org/repo",
    });
    expect(result.valid).toBe(true);
  });

  it("accepts valid git_remote_url with http://", () => {
    const result = validateSlideUpdateInput({
      git_remote_url: "http://github.com/org/repo",
    });
    expect(result.valid).toBe(true);
  });

  it("accepts valid git_remote_url with git://", () => {
    const result = validateSlideUpdateInput({
      git_remote_url: "git://github.com/org/repo.git",
    });
    expect(result.valid).toBe(true);
  });

  it("accepts valid git_remote_url with ssh://", () => {
    const result = validateSlideUpdateInput({
      git_remote_url: "ssh://git@github.com/org/repo.git",
    });
    expect(result.valid).toBe(true);
  });

  it("rejects git_remote_url with disallowed scheme", () => {
    const result = validateSlideUpdateInput({
      git_remote_url: "javascript:alert(1)",
    });
    expect(result.valid).toBe(false);
    if (!result.valid) expect(result.error).toContain("must start with");
  });

  it("rejects git_remote_url with ftp:// scheme", () => {
    const result = validateSlideUpdateInput({
      git_remote_url: "ftp://example.com/repo",
    });
    expect(result.valid).toBe(false);
    if (!result.valid) expect(result.error).toContain("must start with");
  });

  it("rejects empty string git_remote_url", () => {
    const result = validateSlideUpdateInput({ git_remote_url: "" });
    expect(result.valid).toBe(false);
  });

  it("accepts null git_remote_url", () => {
    const result = validateSlideUpdateInput({ git_remote_url: null });
    expect(result.valid).toBe(true);
  });

  it("accepts multiple valid fields together and returns normalized data", () => {
    const gitHash = "a".repeat(40);
    const result = validateSlideUpdateInput({
      project_id: "org/proj",
      notes: "note",
      git_remote_url: "https://github.com/org/repo",
      git_hash: gitHash,
    });
    expect(result.valid).toBe(true);
    if (result.valid) {
      expect(result.data).toEqual({
        project_id: "org/proj",
        notes: "note",
        git_remote_url: "https://github.com/org/repo",
        git_hash: gitHash,
      });
    }
  });

  it("rejects when one field is invalid among valid ones", () => {
    const result = validateSlideUpdateInput({
      project_id: "org/proj",
      git_hash: "invalid",
    });
    expect(result.valid).toBe(false);
  });
});
