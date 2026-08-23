import { describe, it, expect, vi, afterEach } from "vitest";

const add = vi.fn();
vi.mock("@/components/ui/toast", () => ({ toast: { add: (...args: unknown[]) => add(...args) } }));

async function freshErrors() {
  vi.resetModules();
  add.mockClear();
  return import("./errors");
}

afterEach(() => {
  vi.useRealTimers();
});

describe("errMessage", () => {
  it("uses the Error's message", async () => {
    const { errMessage } = await freshErrors();
    expect(errMessage(new Error("queue not found"), "Failed")).toBe("queue not found");
  });

  it("falls back for non-Error throws", async () => {
    const { errMessage } = await freshErrors();
    expect(errMessage("a bare string", "Failed to load")).toBe("Failed to load");
    expect(errMessage(undefined, "Failed to load")).toBe("Failed to load");
  });

  it("returns empty for an expired session so nothing is rendered", async () => {
    const { errMessage } = await freshErrors();
    expect(errMessage(new Error("session expired"), "Failed to load")).toBe("");
  });
});

describe("reportError", () => {
  it("raises a toast carrying both the label and the detail", async () => {
    const { reportError } = await freshErrors();
    reportError(new Error("boom"), "Failed to retry job");

    expect(add).toHaveBeenCalledTimes(1);
    expect(add).toHaveBeenCalledWith({
      title: "Failed to retry job",
      description: "boom",
      type: "error",
    });
  });

  it("stays silent for an expired session", async () => {
    const { reportError } = await freshErrors();
    reportError(new Error("session expired"), "Failed to retry job");
    expect(add).not.toHaveBeenCalled();
  });

  it("suppresses an identical message inside the window", async () => {
    const { reportError } = await freshErrors();
    reportError(new Error("network down"), "Failed to load");
    reportError(new Error("network down"), "Failed to load");
    reportError(new Error("network down"), "Failed to load");

    expect(add).toHaveBeenCalledTimes(1);
  });

  it("still reports a different message right away", async () => {
    const { reportError } = await freshErrors();
    reportError(new Error("network down"), "Failed to load");
    reportError(new Error("queue not found"), "Failed to load");

    expect(add).toHaveBeenCalledTimes(2);
  });

  it("reports the same message again once the window lapses", async () => {
    vi.useFakeTimers();
    const { reportError } = await freshErrors();

    reportError(new Error("network down"), "Failed to load");
    vi.advanceTimersByTime(29_000);
    reportError(new Error("network down"), "Failed to load");
    expect(add).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(2_000);
    reportError(new Error("network down"), "Failed to load");
    expect(add).toHaveBeenCalledTimes(2);
  });
});
