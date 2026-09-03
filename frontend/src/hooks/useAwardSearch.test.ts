import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, postScrape, searchAwards } from "../utils/api.ts";
import type { AwardOption, SearchParams } from "../utils/api.ts";
import { useAwardSearch } from "./useAwardSearch.ts";

vi.mock("../utils/api.ts", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../utils/api.ts")>();
  return { ...actual, postScrape: vi.fn(), searchAwards: vi.fn() };
});

const postScrapeMock = vi.mocked(postScrape);
const searchAwardsMock = vi.mocked(searchAwards);

const PARAMS: SearchParams = {
  origin: "SFO",
  destination: "JFK",
  date: "2099-12-20",
};

const AWARD: AwardOption = {
  airline_code: "united",
  airline_name: "United Airlines",
  cabin: "economy",
  flight_number: "UA1",
  flight_origin: "SFO",
  flight_destination: "JFK",
  depart_time: "2099-12-20T08:00:00Z",
  arrive_time: "2099-12-20T16:30:00Z",
  duration_minutes: 330,
  stops: 0,
  award_type: "dynamic",
  points_cost: 25000,
  taxes_fees: 5.6,
  currency: "USD",
  scraped_at: "2026-09-03T00:00:00Z",
};

beforeEach(() => {
  postScrapeMock.mockReset().mockResolvedValue({
    dispatched: ["united"],
    origin: "SFO",
    destination: "JFK",
    date: "2099-12-20",
  });
  searchAwardsMock.mockReset().mockResolvedValue([AWARD]);
});

afterEach(() => vi.restoreAllMocks());

describe("useAwardSearch", () => {
  it("starts idle", () => {
    const { result } = renderHook(() => useAwardSearch());
    expect(result.current.status).toBe("idle");
    expect(result.current.results).toEqual([]);
    expect(result.current.scrapeWarning).toBeNull();
  });

  it("dispatches a scrape before searching, then exposes the results", async () => {
    const { result } = renderHook(() => useAwardSearch());
    act(() => result.current.run(PARAMS));

    await waitFor(() => expect(result.current.status).toBe("success"));
    expect(result.current.results).toEqual([AWARD]);
    expect(postScrapeMock).toHaveBeenCalledWith(PARAMS);
    expect(searchAwardsMock).toHaveBeenCalledWith(PARAMS);
    expect(postScrapeMock.mock.invocationCallOrder[0]).toBeLessThan(
      searchAwardsMock.mock.invocationCallOrder[0],
    );
  });

  it("still searches when the scrape dispatch fails, and flags it", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    postScrapeMock.mockRejectedValue(new ApiError("kafka down", 502));

    const { result } = renderHook(() => useAwardSearch());
    act(() => result.current.run(PARAMS));

    await waitFor(() => expect(result.current.status).toBe("success"));
    expect(result.current.results).toEqual([AWARD]);
    expect(result.current.scrapeWarning).toMatch(/fresh scrape/i);
  });

  it("clears a stale scrape warning on the next run", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    postScrapeMock.mockRejectedValueOnce(new ApiError("kafka down", 502));

    const { result } = renderHook(() => useAwardSearch());
    act(() => result.current.run(PARAMS));
    await waitFor(() => expect(result.current.scrapeWarning).not.toBeNull());

    act(() => result.current.run(PARAMS));
    await waitFor(() => expect(result.current.status).toBe("success"));
    expect(result.current.scrapeWarning).toBeNull();
  });

  it("surfaces an ApiError from the search", async () => {
    searchAwardsMock.mockRejectedValue(new ApiError("invalid cabin", 400));

    const { result } = renderHook(() => useAwardSearch());
    act(() => result.current.run(PARAMS));

    await waitFor(() => expect(result.current.status).toBe("error"));
    expect(result.current.error).toBe("invalid cabin");
    expect(result.current.results).toEqual([]);
  });
});
