import { describe, expect, it } from "vitest";
import { AIRPORTS, formatAirport } from "./airports.ts";

describe("AIRPORTS", () => {
  it("is a non-empty list", () => {
    expect(AIRPORTS.length).toBeGreaterThan(0);
  });

  it("every code is three uppercase letters", () => {
    for (const a of AIRPORTS) {
      expect(a.code).toMatch(/^[A-Z]{3}$/);
    }
  });

  it("has no duplicate codes", () => {
    const codes = AIRPORTS.map((a) => a.code);
    expect(new Set(codes).size).toBe(codes.length);
  });

  it("every entry has a city and name", () => {
    for (const a of AIRPORTS) {
      expect(a.city).not.toBe("");
      expect(a.name).not.toBe("");
    }
  });
});

describe("formatAirport", () => {
  it("leads with the code", () => {
    expect(formatAirport({ code: "SFO", name: "San Francisco International", city: "San Francisco" })).toBe(
      "SFO — San Francisco (San Francisco International)",
    );
  });
});
