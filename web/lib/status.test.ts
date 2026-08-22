import { describe, it, expect, vi, afterEach } from "vitest";
import { fmtDuration, fmtRelative, fmtDate, STATUS_COLOR } from "./status";

afterEach(() => {
  vi.useRealTimers();
});

describe("fmtDuration", () => {
  it("renders sub-second values as whole milliseconds", () => {
    expect(fmtDuration(30)).toBe("30ms");
    expect(fmtDuration(999)).toBe("999ms");
  });

  // The metrics endpoint returns avg_duration_ms as a float, which used to be
  // interpolated raw and rendered as "30.402843601895736ms".
  it("rounds a fractional millisecond average", () => {
    expect(fmtDuration(30.402843601895736)).toBe("30ms");
    expect(fmtDuration(0.6)).toBe("1ms");
  });

  it("switches to seconds and minutes at the thresholds", () => {
    expect(fmtDuration(1000)).toBe("1.0s");
    expect(fmtDuration(59_999)).toBe("60.0s");
    expect(fmtDuration(60_000)).toBe("1.0m");
    expect(fmtDuration(90_000)).toBe("1.5m");
  });

  it("shows a dash when there is no measurement", () => {
    expect(fmtDuration(null)).toBe("–");
    expect(fmtDuration(undefined)).toBe("–");
  });

  // Zero is a real measurement, not a missing one.
  it("does not confuse zero with absent", () => {
    expect(fmtDuration(0)).toBe("0ms");
  });
});

describe("fmtRelative", () => {
  it("picks the unit that fits the gap", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T12:00:00Z"));

    expect(fmtRelative("2026-01-01T11:59:56Z")).toBe("4s ago");
    expect(fmtRelative("2026-01-01T11:45:00Z")).toBe("15m ago");
    expect(fmtRelative("2026-01-01T06:00:00Z")).toBe("6h ago");
    expect(fmtRelative("2025-12-29T12:00:00Z")).toBe("3d ago");
  });

  it("shows a dash for a missing timestamp", () => {
    expect(fmtRelative(null)).toBe("–");
    expect(fmtRelative(undefined)).toBe("–");
    expect(fmtRelative("")).toBe("–");
  });
});

describe("fmtDate", () => {
  it("shows a dash for a missing timestamp", () => {
    expect(fmtDate(null)).toBe("–");
    expect(fmtDate(undefined)).toBe("–");
  });

  it("formats a timestamp without throwing", () => {
    expect(fmtDate("2026-01-01T12:00:00Z")).toMatch(/Jan/);
  });
});

describe("STATUS_COLOR", () => {
  // Every status the API can return needs an entry, or the badge renders bare.
  it("covers every job and worker status", () => {
    const statuses = [
      "queued", "scheduled", "claimed", "running",
      "completed", "failed", "cancelled", "dead",
      "active", "draining", "offline",
    ];
    for (const s of statuses) {
      expect(STATUS_COLOR[s], `missing colour for "${s}"`).toBeTruthy();
    }
  });
});
