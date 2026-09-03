import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  API_BASE_URL,
  ApiError,
  getHealth,
  postScrape,
  searchAwards,
} from "./api.ts";

function mockFetch(response: {
  ok?: boolean;
  status?: number;
  body?: unknown;
}) {
  const text =
    response.body === undefined ? "" : JSON.stringify(response.body);
  const fn = vi.fn().mockResolvedValue({
    ok: response.ok ?? true,
    status: response.status ?? 200,
    text: () => Promise.resolve(text),
  } as Response);
  vi.stubGlobal("fetch", fn);
  return fn;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  vi.resetModules();
});

describe("API_BASE_URL", () => {
  it("is exposed without a trailing slash", () => {
    expect(API_BASE_URL).not.toMatch(/\/$/);
  });

  it("falls back to localhost:8080 when VITE_API_BASE_URL is unset", async () => {
    vi.stubEnv("VITE_API_BASE_URL", "");
    vi.resetModules();
    const fresh = await import("./api.ts");
    expect(fresh.API_BASE_URL).toBe("http://localhost:8080");
  });

  it("reads VITE_API_BASE_URL and strips a trailing slash", async () => {
    vi.stubEnv("VITE_API_BASE_URL", "https://api.example.com/");
    vi.resetModules();
    const fresh = await import("./api.ts");
    expect(fresh.API_BASE_URL).toBe("https://api.example.com");
  });
});

describe("apiFetch", () => {
  beforeEach(() => {
    vi.stubEnv("VITE_API_BASE_URL", "http://localhost:8080");
  });

  it("hits the health endpoint and returns the parsed body", async () => {
    const fetchMock = mockFetch({ body: { status: "ok" } });
    await expect(getHealth()).resolves.toEqual({ status: "ok" });
    expect(fetchMock).toHaveBeenCalledWith(
      "http://localhost:8080/healthz",
      expect.objectContaining({
        headers: expect.objectContaining({ Accept: "application/json" }),
      }),
    );
  });

  it("builds the search query string and omits an unset cabin", async () => {
    const fetchMock = mockFetch({ body: [] });
    await searchAwards({ origin: "BOS", destination: "SFO", date: "2026-12-20" });
    expect(fetchMock.mock.calls[0][0]).toBe(
      "http://localhost:8080/api/search?origin=BOS&destination=SFO&date=2026-12-20",
    );
  });

  it("includes cabin when provided", async () => {
    const fetchMock = mockFetch({ body: [] });
    await searchAwards({
      origin: "BOS",
      destination: "SFO",
      date: "2026-12-20",
      cabin: "business",
    });
    expect(fetchMock.mock.calls[0][0]).toContain("cabin=business");
  });

  it("sends POST /api/scrape with a JSON body and content-type", async () => {
    const fetchMock = mockFetch({
      status: 202,
      body: {
        dispatched: ["delta"],
        origin: "SEA",
        destination: "JFK",
        date: "2026-12-08",
      },
    });
    await postScrape({
      origin: "SEA",
      destination: "JFK",
      date: "2026-12-08",
      airlines: ["delta"],
    });
    const [, init] = fetchMock.mock.calls[0];
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body)).toEqual({
      origin: "SEA",
      destination: "JFK",
      date: "2026-12-08",
      airlines: ["delta"],
    });
    expect(init.headers["Content-Type"]).toBe("application/json");
  });

  it("throws ApiError carrying the {error} envelope message and status", async () => {
    mockFetch({ ok: false, status: 400, body: { error: "invalid cabin" } });
    await expect(
      searchAwards({ origin: "BOS", destination: "SFO", date: "bad" }),
    ).rejects.toMatchObject({
      name: "ApiError",
      message: "invalid cabin",
      status: 400,
    });
  });

  it("throws ApiError with status 0 when fetch itself rejects", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockRejectedValue(new TypeError("network down")),
    );
    const err = await getHealth().catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(0);
  });
});
