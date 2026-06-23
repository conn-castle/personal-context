// @vitest-environment jsdom
// Pin a negative-UTC-offset zone (west of UTC). `new Date("YYYY-MM-DD")` parses
// ISO date-only strings as UTC midnight, whose local calendar day is the
// PREVIOUS day west of UTC — the typed-input timezone bug under test.
process.env.TZ = "America/New_York";

import { afterEach, beforeAll, describe, expect, it } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";

import { RecordDatePicker } from "@/components/record-date-picker";
import type { RecordSummary } from "@/lib/types";

function makeRecord(id: string, date: string): RecordSummary {
  return {
    id,
    date,
    day_order: "a0",
    html_content: null,
    project_id: "proj",
    source_device_id: "dev",
    source_ref: null,
    updated_at: "2026-03-07T00:00:00.000Z",
    deleted_at: null,
    figure_count: 0,
    data_file_count: 0,
  };
}

const records = [makeRecord("r-07", "2026-03-07")];

beforeAll(() => {
  // Radix Popover needs these DOM APIs that jsdom does not implement.
  if (!Element.prototype.hasPointerCapture) {
    Element.prototype.hasPointerCapture = () => false;
  }
  if (!Element.prototype.setPointerCapture) {
    Element.prototype.setPointerCapture = () => undefined;
  }
  if (!Element.prototype.releasePointerCapture) {
    Element.prototype.releasePointerCapture = () => undefined;
  }
  if (!Element.prototype.scrollIntoView) {
    Element.prototype.scrollIntoView = () => undefined;
  }
});

describe("RecordDatePicker", () => {
  afterEach(() => {
    cleanup();
  });

  it("gives the icon-only trigger an accessible name", () => {
    render(<RecordDatePicker records={records} onSelectDate={() => undefined} />);

    // The trigger renders only a CalendarDays icon. Its accessible name must
    // come from aria-label, not the portaled tooltip (which Radix does not wire
    // to the trigger via aria-labelledby). Without the aria-label this query
    // matches nothing and the test fails.
    expect(
      screen.getByRole("button", { name: "Jump to date" })
    ).toBeTruthy();
  });

  it("clears stale input text and the error border when the popover closes", () => {
    render(<RecordDatePicker records={records} onSelectDate={() => undefined} />);

    const openPopover = () =>
      fireEvent.click(screen.getByRole("button", { name: "Jump to date" }));

    // The error state is signalled by the bare `border-destructive` utility
    // class (the picker applies `'border-destructive flex-1'` when inputError
    // is set). The Input base classes already include the unrelated
    // `aria-invalid:border-destructive` variant, so match the standalone token
    // with a word boundary to avoid a false positive.
    const hasErrorBorder = (el: HTMLInputElement) =>
      / border-destructive\b/.test(` ${el.className}`);

    openPopover();
    const input = screen.getByPlaceholderText("e.g. 2025-03-05") as HTMLInputElement;
    // Enter an invalid date and submit so the error border is applied.
    fireEvent.change(input, { target: { value: "not-a-date" } });
    fireEvent.submit(input.closest("form")!);
    expect(input.value).toBe("not-a-date");
    expect(hasErrorBorder(input)).toBe(true);

    // Dismiss the popover without submitting a valid date (Escape simulates the
    // outside-click/Escape path that does not run the submit/select reset).
    fireEvent.keyDown(input, { key: "Escape", code: "Escape" });

    // Reopen: the input must be fresh — no stale text and no error border.
    openPopover();
    const reopened = screen.getByPlaceholderText(
      "e.g. 2025-03-05"
    ) as HTMLInputElement;
    expect(reopened.value).toBe("");
    expect(hasErrorBorder(reopened)).toBe(false);
  });

  it("selects the typed ISO date on its local calendar day, not the UTC day", () => {
    const selected: Date[] = [];
    render(
      <RecordDatePicker
        records={records}
        onSelectDate={(date) => selected.push(date)}
      />
    );

    fireEvent.click(screen.getByRole("button", { name: "Jump to date" }));
    const input = screen.getByPlaceholderText("e.g. 2025-03-05") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "2026-03-07" } });
    fireEvent.submit(input.closest("form")!);

    expect(selected).toHaveLength(1);
    const picked = selected[0];
    // The old `new Date("2026-03-07")` parsed as UTC midnight, whose local day
    // west of UTC is 2026-03-06. The selected Date's LOCAL calendar day must be
    // the day the user typed.
    const localKey = `${picked.getFullYear()}-${String(picked.getMonth() + 1).padStart(2, "0")}-${String(picked.getDate()).padStart(2, "0")}`;
    expect(localKey).toBe("2026-03-07");
  });
});
