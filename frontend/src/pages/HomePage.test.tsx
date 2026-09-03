import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AwardOption } from "../utils/api.ts";
import type { AwardSearch } from "../hooks/useAwardSearch.ts";
import { useAwardSearch } from "../hooks/useAwardSearch.ts";
import { HomePage } from "./HomePage.tsx";

vi.mock("../hooks/useAwardSearch.ts", () => ({ useAwardSearch: vi.fn() }));
vi.mock("../components/ApiStatusBadge.tsx", () => ({
  ApiStatusBadge: () => null,
}));

const useAwardSearchMock = vi.mocked(useAwardSearch);

const run = vi.fn();

function mockSearch(overrides: Partial<AwardSearch>) {
  const base: AwardSearch = {
    status: "idle",
    results: [],
    error: null,
    scrapeWarning: null,
    run,
  };
  useAwardSearchMock.mockReturnValue({ ...base, ...overrides });
}

const AWARD: AwardOption = {
  airline_code: "united",
  airline_name: "United Airlines",
  cabin: "economy",
  flight_number: "UA1",
  flight_origin: "SFO",
  flight_destination: "JFK",
  depart_time: "2026-12-20T08:00:00Z",
  arrive_time: "2026-12-20T16:30:00Z",
  duration_minutes: 330,
  stops: 0,
  award_type: "dynamic",
  points_cost: 25000,
  taxes_fees: 5.6,
  currency: "USD",
  scraped_at: "2026-09-03T00:00:00Z",
};

/** Fill and submit the search form so `submitted` params exist. */
function search() {
  fireEvent.change(screen.getByLabelText("Origin"), {
    target: { value: "SFO" },
  });
  fireEvent.change(screen.getByLabelText("Destination"), {
    target: { value: "JFK" },
  });
  fireEvent.change(screen.getByLabelText("Date"), {
    target: { value: "2099-12-20" },
  });
  fireEvent.click(screen.getByRole("button", { name: /search awards/i }));
}

beforeEach(() => {
  run.mockReset();
  useAwardSearchMock.mockReset();
});
afterEach(cleanup);

describe("HomePage", () => {
  it("renders the error state and retries the last search", () => {
    mockSearch({ status: "error", error: "invalid cabin" });
    render(<HomePage />);
    search();
    expect(run).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: /try again/i }));
    expect(run).toHaveBeenCalledTimes(2);
    expect(run.mock.calls[1][0]).toEqual({
      origin: "SFO",
      destination: "JFK",
      date: "2099-12-20",
    });
  });

  it("renders the empty state after a search with no results", () => {
    mockSearch({ status: "success", results: [] });
    render(<HomePage />);
    search();
    expect(screen.getByText(/no award options found/i)).toBeInTheDocument();
  });

  it("shows the scrape warning above the results table", () => {
    mockSearch({
      status: "success",
      results: [AWARD],
      scrapeWarning: "Couldn't queue a fresh scrape — showing results already on file.",
    });
    render(<HomePage />);
    search();
    expect(screen.getByText(/couldn't queue a fresh scrape/i)).toBeInTheDocument();
    expect(screen.getByRole("table")).toBeInTheDocument();
  });
});
