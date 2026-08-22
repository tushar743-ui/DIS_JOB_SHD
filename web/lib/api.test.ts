import { describe, it, expect, vi, afterEach } from "vitest";

// Reset the module between tests: api.ts keeps the tokens and the in-flight
// refresh promise in module scope, so state would otherwise leak across cases.
async function freshApi() {
  vi.resetModules();
  return import("./api");
}

type Handler = (url: string, init: RequestInit) => Response | Promise<Response>;

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** Installs a fetch stub and records every call it receives. */
function stubFetch(handler: Handler) {
  const calls: { url: string; init: RequestInit }[] = [];
  const spy = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const url = String(input);
    calls.push({ url, init });
    return handler(url, init);
  });
  vi.stubGlobal("fetch", spy);
  return { calls, spy };
}

function authHeader(init: RequestInit): string | undefined {
  return (init.headers as Record<string, string> | undefined)?.Authorization;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("request auth", () => {
  it("sends the access token as a bearer header", async () => {
    const api = await freshApi();
    const { calls } = stubFetch(() => json([]));
    api.setAccessToken("access-1");

    await api.orgs.list();

    expect(authHeader(calls[0].init)).toBe("Bearer access-1");
  });

  it("omits the header entirely when there is no token", async () => {
    const api = await freshApi();
    const { calls } = stubFetch(() => json([]));

    await api.orgs.list();

    expect(authHeader(calls[0].init)).toBeUndefined();
  });

  it("returns undefined for a 204 instead of parsing an empty body", async () => {
    const api = await freshApi();
    stubFetch(() => new Response(null, { status: 204 }));
    api.setAccessToken("access-1");

    await expect(api.queues.pause("q1")).resolves.toBeUndefined();
  });

  it("surfaces the server's error message", async () => {
    const api = await freshApi();
    stubFetch(() => json({ error: "queue not found" }, 404));
    api.setAccessToken("access-1");

    await expect(api.queues.get("nope")).rejects.toThrow("queue not found");
  });

  it("falls back to the status text when the error body is not JSON", async () => {
    const api = await freshApi();
    stubFetch(() => new Response("<html>502</html>", { status: 502, statusText: "Bad Gateway" }));
    api.setAccessToken("access-1");

    await expect(api.queues.get("q1")).rejects.toThrow("Bad Gateway");
  });
});

describe("token refresh", () => {
  it("refreshes on 401 and replays the request with the new token", async () => {
    const api = await freshApi();
    const { calls } = stubFetch((url, init) => {
      if (url.endsWith("/auth/refresh")) {
        return json({ access_token: "access-2", refresh_token: "refresh-2" });
      }
      if (authHeader(init) === "Bearer access-1") {
        return json({ error: "invalid token" }, 401);
      }
      return json([{ id: "q1" }]);
    });
    api.setAccessToken("access-1");
    api.setRefreshToken("refresh-1");

    const result = await api.orgs.list();

    expect(result).toEqual([{ id: "q1" }]);
    // original 401 -> refresh -> replay
    expect(calls.map((c) => c.url.split("/api/v1")[1])).toEqual([
      "/orgs",
      "/auth/refresh",
      "/orgs",
    ]);
    expect(authHeader(calls[2].init)).toBe("Bearer access-2");
  });

  it("hands the rotated tokens to the listener so they can be persisted", async () => {
    const api = await freshApi();
    stubFetch((url, init) => {
      if (url.endsWith("/auth/refresh")) {
        return json({ access_token: "access-2", refresh_token: "refresh-2" });
      }
      return authHeader(init) === "Bearer access-1" ? json({ error: "invalid token" }, 401) : json([]);
    });
    const onTokens = vi.fn();
    api.onTokensRefreshed(onTokens);
    api.setAccessToken("access-1");
    api.setRefreshToken("refresh-1");

    await api.orgs.list();

    expect(onTokens).toHaveBeenCalledWith("access-2", "refresh-2");
  });

  // The server revokes a refresh token the moment it is redeemed. If each
  // concurrent 401 started its own refresh, the first would win and the rest
  // would present an already-dead token and log the user out.
  it("redeems the refresh token only once for concurrent 401s", async () => {
    const api = await freshApi();
    let refreshCount = 0;
    let usedRefreshToken = "";
    const { calls } = stubFetch(async (url, init) => {
      if (url.endsWith("/auth/refresh")) {
        refreshCount += 1;
        const body = JSON.parse(String(init.body)) as { refresh_token: string };
        if (usedRefreshToken === body.refresh_token) {
          // Mirrors the real API replaying a revoked token.
          return json({ error: "invalid or expired refresh token" }, 401);
        }
        usedRefreshToken = body.refresh_token;
        await new Promise((r) => setTimeout(r, 10));
        return json({ access_token: "access-2", refresh_token: "refresh-2" });
      }
      return authHeader(init) === "Bearer access-1" ? json({ error: "invalid token" }, 401) : json([]);
    });
    api.setAccessToken("access-1");
    api.setRefreshToken("refresh-1");

    // Six pages loading at once, exactly like the dashboard on first paint.
    await Promise.all([
      api.orgs.list(),
      api.orgs.list(),
      api.queues.list("p1"),
      api.workers.list("p1"),
      api.metrics.project("p1"),
      api.jobs.list("q1"),
    ]);

    expect(refreshCount).toBe(1);
    const refreshCalls = calls.filter((c) => c.url.endsWith("/auth/refresh"));
    expect(refreshCalls).toHaveLength(1);
  });

  it("allows a later refresh after the in-flight one settles", async () => {
    const api = await freshApi();
    let refreshCount = 0;
    let current = "access-1";
    stubFetch((url, init) => {
      if (url.endsWith("/auth/refresh")) {
        refreshCount += 1;
        current = `access-${refreshCount + 1}`;
        return json({ access_token: current, refresh_token: `refresh-${refreshCount + 1}` });
      }
      return authHeader(init) === `Bearer ${current}` ? json([]) : json({ error: "invalid token" }, 401);
    });
    api.setAccessToken("stale");
    api.setRefreshToken("refresh-1");

    await api.orgs.list();
    api.setAccessToken("stale-again");
    await api.orgs.list();

    // The single-flight latch must not stay stuck closed after it resolves.
    expect(refreshCount).toBe(2);
  });

  it("signals auth failure and stops when the refresh itself is rejected", async () => {
    const api = await freshApi();
    const { calls } = stubFetch((url) =>
      url.endsWith("/auth/refresh")
        ? json({ error: "invalid or expired refresh token" }, 401)
        : json({ error: "invalid token" }, 401)
    );
    const onAuthLost = vi.fn();
    api.onAuthFailure(onAuthLost);
    api.setAccessToken("access-1");
    api.setRefreshToken("refresh-1");

    await expect(api.orgs.list()).rejects.toThrow("session expired");
    expect(onAuthLost).toHaveBeenCalledTimes(1);
    // No pointless replay of the original request.
    expect(calls).toHaveLength(2);
  });

  it("does not attempt a refresh when there is no refresh token", async () => {
    const api = await freshApi();
    const { calls } = stubFetch(() => json({ error: "invalid token" }, 401));
    const onAuthLost = vi.fn();
    api.onAuthFailure(onAuthLost);
    api.setAccessToken("access-1");

    await expect(api.orgs.list()).rejects.toThrow("invalid token");
    expect(calls).toHaveLength(1);
    expect(onAuthLost).toHaveBeenCalledTimes(1);
  });

  // A 401 from these means "wrong credentials", not "expired session"; retrying
  // them through the refresh path would be wrong.
  it("never refreshes on login or register", async () => {
    const api = await freshApi();
    const { calls } = stubFetch(() => json({ error: "invalid credentials" }, 401));
    api.setRefreshToken("refresh-1");

    await expect(api.auth.login("a@b.test", "wrong")).rejects.toThrow("invalid credentials");
    await expect(api.auth.register("a@b.test", "pw", "A")).rejects.toThrow("invalid credentials");

    expect(calls.some((c) => c.url.endsWith("/auth/refresh"))).toBe(false);
  });

  it("retries only once, so a persistently rejecting server cannot loop", async () => {
    const api = await freshApi();
    const { calls } = stubFetch((url) =>
      url.endsWith("/auth/refresh")
        ? json({ access_token: "access-2", refresh_token: "refresh-2" })
        : json({ error: "invalid token" }, 401)
    );
    const onAuthLost = vi.fn();
    api.onAuthFailure(onAuthLost);
    api.setAccessToken("access-1");
    api.setRefreshToken("refresh-1");

    await expect(api.orgs.list()).rejects.toThrow("invalid token");

    // request -> refresh -> replay, then give up.
    expect(calls).toHaveLength(3);
    expect(onAuthLost).toHaveBeenCalledTimes(1);
  });
});

describe("query building", () => {
  it("omits the status filter when listing all jobs", async () => {
    const api = await freshApi();
    const { calls } = stubFetch(() => json({ data: [], total: 0, limit: 25, offset: 0 }));
    api.setAccessToken("access-1");

    await api.jobs.list("q1", { limit: 25, offset: 50 });

    expect(calls[0].url).toContain("limit=25");
    expect(calls[0].url).toContain("offset=50");
    expect(calls[0].url).not.toContain("status=");
  });
});
