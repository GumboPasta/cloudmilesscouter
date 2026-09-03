import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ResultsTable } from "./ResultsTable.tsx";
import type { AwardOption } from "../utils/api.ts";

afterEach(cleanup);

const NAMES: Record<string, string> = {
  united: "United Airlines",
  american: "American Airlines",
  delta: "Delta Air Lines",
  alaska: "Alaska Airlines",
};

function opt(o: Partial<AwardOption> & { airline_code: string }): AwardOption {
  return {
    airline_name: NAMES[o.airline_code] ?? o.airline_code,
    cabin: "economy",
    flight_number: "XX1",
    flight_origin: "SFO",
    flight_destination: "JFK",
    depart_time: "2026-12-20T08:00:00Z",
    arrive_time: "2026-12-20T16:00:00Z",
    duration_minutes: 360,
    stops: 0,
    award_type: "saver",
    points_cost: 30000,
    taxes_fees: 5.6,
    currency: "USD",
    scraped_at: "2026-09-01T00:00:00Z",
    ...o,
  };
}

const RESULTS: AwardOption[] = [
  opt({ airline_code: "united", cabin: "business", points_cost: 80000 }),
  opt({ airline_code: "american", cabin: "economy", points_cost: 25000 }),
  opt({ airline_code: "delta", cabin: "economy", points_cost: 40000 }),
  opt({ airline_code: "alaska", cabin: "business", points_cost: 55000 }),
];

/** Airline-name cell of every body row, top to bottom. */
function airlineColumn(): string[] {
  const table = screen.getByRole("table");
  return within(table)
    .getAllByRole("row")
    .slice(1)
    .map((row) => within(row).getAllByRole("cell")[0].textContent ?? "");
}

describe("ResultsTable", () => {
  it("renders one row per result, points ascending by default", () => {
    render(<ResultsTable results={RESULTS} />);
    expect(airlineColumn()).toEqual([
      "American Airlines",
      "Delta Air Lines",
      "Alaska Airlines",
      "United Airlines",
    ]);
  });

  it("reverses order when the active sort header is clicked", () => {
    render(<ResultsTable results={RESULTS} />);
    fireEvent.click(screen.getByRole("button", { name: /Points/i }));
    expect(airlineColumn()).toEqual([
      "United Airlines",
      "Alaska Airlines",
      "Delta Air Lines",
      "American Airlines",
    ]);
  });

  it("sorts by airline when that header is clicked", () => {
    render(<ResultsTable results={RESULTS} />);
    fireEvent.click(screen.getByRole("button", { name: /Airline/i }));
    expect(airlineColumn()).toEqual([
      "Alaska Airlines",
      "American Airlines",
      "Delta Air Lines",
      "United Airlines",
    ]);
  });

  it("filters by airline", () => {
    render(<ResultsTable results={RESULTS} />);
    fireEvent.change(screen.getByRole("combobox", { name: "Airline" }), {
      target: { value: "american" },
    });
    expect(airlineColumn()).toEqual(["American Airlines"]);
  });

  it("filters by cabin", () => {
    render(<ResultsTable results={RESULTS} />);
    fireEvent.change(screen.getByRole("combobox", { name: "Cabin" }), {
      target: { value: "economy" },
    });
    expect(airlineColumn()).toEqual(["American Airlines", "Delta Air Lines"]);
  });

  it("filters by alliance", () => {
    render(<ResultsTable results={RESULTS} />);
    fireEvent.change(screen.getByRole("combobox", { name: "Alliance" }), {
      target: { value: "oneworld" },
    });
    expect(airlineColumn()).toEqual(["American Airlines", "Alaska Airlines"]);
  });

  it("shows a message when filters exclude everything", () => {
    render(<ResultsTable results={RESULTS} />);
    fireEvent.change(screen.getByRole("combobox", { name: "Airline" }), {
      target: { value: "american" },
    });
    fireEvent.change(screen.getByRole("combobox", { name: "Cabin" }), {
      target: { value: "business" },
    });
    expect(screen.getByText(/no results match these filters/i)).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });
});
