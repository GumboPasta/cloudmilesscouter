import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { EmptyState } from "./EmptyState.tsx";

afterEach(cleanup);

describe("EmptyState", () => {
  it("names the searched route and date", () => {
    render(
      <EmptyState
        params={{ origin: "SFO", destination: "JFK", date: "2026-12-20" }}
      />,
    );
    expect(screen.getByText(/no award options found/i)).toBeInTheDocument();
    expect(screen.getByText(/SFO → JFK on 2026-12-20/)).toBeInTheDocument();
  });

  it("mentions the cabin when one was searched", () => {
    render(
      <EmptyState
        params={{
          origin: "SFO",
          destination: "JFK",
          date: "2026-12-20",
          cabin: "premium_economy",
        }}
      />,
    );
    expect(screen.getByText(/in premium economy/)).toBeInTheDocument();
  });
});
